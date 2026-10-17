package tunbridge

import (
	"encoding/binary"
	"sync"
	"sync/atomic"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/nested"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// Обманки средств обхода DPI.
//
// zapret и GoodbyeDPI работают на том же компьютере, что и туннель, и
// перехватывают пакеты на всех интерфейсах — в том числе на нашем адаптере.
// Перед настоящим приветствием TLS они шлют поддельное: с чужим именем сайта
// и с заведомо негодной приметой — устаревшей меткой времени TCP (ts),
// опцией MD5 (md5sig) или TTL, которого не хватит до сервера (ttl). Фильтр
// провайдера читает подделку и путается, а сервер её выбрасывает: PAWS
// отбрасывает устаревшую метку, MD5 без договорённости не принимается,
// TTL кончается по дороге.
//
// У туннеля сервер — это стек gvisor прямо за адаптером. Пакет до него
// доходит за ноль шагов, а устаревшую метку времени gvisor не проверяет.
// Подделка принималась за данные, приветствие к YouTube приходило испорченным,
// и браузер писал SSL_ERROR_RX_MALFORMED_SERVER_HELLO. Hiddify с тем же
// zapret работала, потому что стояла прокси: браузер ходил на 127.0.0.1, и
// подделывать было нечего.
//
// Выключать zapret ради туннеля нельзя: без VPN он человеку нужен, а Discord
// и игры он чинит и при включённом. Поэтому мост ведёт себя так, как вёл бы
// себя настоящий сервер: выбрасывает то, что выбросил бы и тот. Подделка
// умирает здесь, настоящие пакеты идут дальше, и zapret с туннелем живут
// рядом. Битую контрольную сумму (badsum) и чужой номер последовательности
// (badseq) gvisor отбрасывает сам.

// decoyTTL — TTL, с которым пакет до сервера через интернет не доходит.
// Windows шлёт 128, Linux и Android — 64; подделки ставят единицы, чтобы
// пакет умер сразу за фильтром провайдера.
const decoyTTL = 8

// maxFlows — сколько соединений помнить. Соединение забывается по FIN и
// RST; брошенные без них вычищаются разом, когда их становится слишком много.
const maxFlows = 16384

type flowKey struct {
	src, dst [16]byte
	sp, dp   uint16
}

// flowTS — последняя метка времени соединения; none — договорились без меток.
type flowTS struct {
	ts   uint32
	none bool
}

// decoyGuard стоит между адаптером и стеком и выбрасывает обманки.
type decoyGuard struct {
	nested.Endpoint
	mu      sync.Mutex
	flows   map[flowKey]flowTS
	dropped atomic.Int64
}

func guardDecoys(child stack.LinkEndpoint) *decoyGuard {
	g := &decoyGuard{flows: make(map[flowKey]flowTS)}
	g.Endpoint.Init(child, g)
	return g
}

// DeliverNetworkPacket — пакет от системы к стеку.
func (g *decoyGuard) DeliverNetworkPacket(protocol tcpip.NetworkProtocolNumber, pkt *stack.PacketBuffer) {
	if g.decoy(head(pkt)) {
		g.dropped.Add(1)
		return
	}
	g.Endpoint.DeliverNetworkPacket(protocol, pkt)
}

// head — начало пакета, где лежат заголовки. Обычно пакет — один кусок, и
// копировать его ради заголовков незачем.
func head(pkt *stack.PacketBuffer) []byte {
	parts := pkt.AsSlices()
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 || len(parts[0]) >= 120 {
		return parts[0]
	}
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
		if len(b) >= 120 {
			break
		}
	}
	return b
}

// decoy разбирает IP и TCP сам: заголовки ещё не разобраны стеком, а
// разбирать за него ради трёх полей — дешевле, чем копировать пакет.
func (g *decoyGuard) decoy(b []byte) bool {
	if len(b) < 20 {
		return false
	}
	var (
		k       flowKey
		tcp     []byte
		ttl     int
		payload int
	)
	switch b[0] >> 4 {
	case 4:
		ihl := int(b[0]&0x0f) * 4
		total := int(binary.BigEndian.Uint16(b[2:4]))
		// Только TCP и только первый фрагмент: в остальных заголовка TCP нет.
		if b[9] != 6 || ihl < 20 || len(b) < ihl+20 || binary.BigEndian.Uint16(b[6:8])&0x1fff != 0 {
			return false
		}
		ttl = int(b[8])
		copy(k.src[:], b[12:16])
		copy(k.dst[:], b[16:20])
		tcp = b[ihl:]
		payload = total - ihl
	case 6:
		// Цепочки расширенных заголовков не разбираем: обманки их не ставят.
		if len(b) < 60 || b[6] != 6 {
			return false
		}
		ttl = int(b[7])
		copy(k.src[:], b[8:24])
		copy(k.dst[:], b[24:40])
		tcp = b[40:]
		payload = int(binary.BigEndian.Uint16(b[4:6]))
	default:
		return false
	}
	if ttl <= decoyTTL {
		return true
	}

	off := int(tcp[12]>>4) * 4
	if off < 20 || len(tcp) < off {
		return false
	}
	payload -= off
	k.sp = binary.BigEndian.Uint16(tcp[0:2])
	k.dp = binary.BigEndian.Uint16(tcp[2:4])
	flags := tcp[13]
	syn, fin, rst := flags&0x02 != 0, flags&0x01 != 0, flags&0x04 != 0

	ts, hasTS, md5 := tcpOptions(tcp[20:off])
	if md5 {
		return true
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	seen, known := g.flows[k]
	switch {
	case syn:
		if len(g.flows) >= maxFlows {
			clear(g.flows)
		}
		g.flows[k] = flowTS{ts: ts, none: !hasTS}
		return false
	case !known:
		return false
	case seen.none && hasTS && (payload > 0 || rst):
		// Договорились без меток, а метка появилась — её поставил не браузер.
		return true
	case hasTS && !seen.none && int32(ts-seen.ts) < 0 && (payload > 0 || rst):
		// Метка старше уже виденной: настоящий сервер выбросил бы такой
		// сегмент по PAWS. Пустые подтверждения не трогаем — они ничего не
		// несут и стеку не вредят.
		return true
	}
	if hasTS && int32(ts-seen.ts) > 0 {
		seen.ts = ts
		g.flows[k] = seen
	}
	if fin || rst {
		delete(g.flows, k)
	}
	return false
}

// tcpOptions достаёт метку времени и признак MD5 из опций TCP.
func tcpOptions(opts []byte) (ts uint32, hasTS, md5 bool) {
	for i := 0; i < len(opts); {
		switch kind := opts[i]; kind {
		case 0:
			return
		case 1:
			i++
			continue
		default:
			if i+1 >= len(opts) {
				return
			}
			n := int(opts[i+1])
			if n < 2 || i+n > len(opts) {
				return
			}
			switch kind {
			case 8:
				if n == 10 {
					ts, hasTS = binary.BigEndian.Uint32(opts[i+2:i+6]), true
				}
			case 19:
				md5 = true
			}
			i += n
		}
	}
	return
}

// Decoys — сколько обманок мост выбросил с начала работы. Журнал называет
// это число, чтобы было видно: zapret рядом есть, и туннель с ним уживается.
func (b *Bridge) Decoys() int64 {
	if b == nil || b.guard == nil {
		return 0
	}
	return b.guard.dropped.Load()
}
