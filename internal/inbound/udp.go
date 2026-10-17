package inbound

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

// UDP поверх VLESS и Trojan.
//
// Без него ссылки vless:// и trojan:// у Happ, v2RayTun и Hiddify открывали
// сайты, но не игры, голосовые звонки и QUIC: нода отвечала на UDP отказом.
// Человек видел «YouTube работает, Roblox не грузит» и винил VPN — и был прав.
//
// Клиенты на Xray шлют UDP двумя способами. На порты 53 и 443 — простой
// командой UDP: один поток — один собеседник, датаграммы с длиной впереди,
// ровно как у датаграмм VP1. На остальные — XUDP: команда mux.cool, и в
// кадрах потока у каждой датаграммы свой адрес. Мультиплексирование TCP
// через mux.cool нода не поддерживает — у Xray оно выключено по умолчанию,
// и такой кадр закрывает поток.
// Так Xray даёт игре одну «полноконусную» точку на много серверов. Trojan
// пишет адрес в каждый пакет всегда.

// DialUDP открывает «соединение» UDP до собеседника. Нода подставляет сюда
// свой исходящий дозвон: закрытые адреса отсекаются там же, где и для TCP.
type DialUDP func(to vp1.Address) (net.Conn, error)

// maxPeers — сколько разных собеседников держит один поток. Игре хватает
// единиц; без потолка один клиент открыл бы на ноде тысячи сокетов.
const maxPeers = 64

// fanout — несколько собеседников UDP за одним потоком клиента.
type fanout struct {
	dial  DialUDP
	idle  time.Duration
	write func(from vp1.Address, payload []byte) error

	mu     sync.Mutex
	peers  map[string]net.Conn
	closed bool
	wmu    sync.Mutex
	last   atomic.Int64
}

func newFanout(dial DialUDP, idle time.Duration, write func(vp1.Address, []byte) error) *fanout {
	f := &fanout{dial: dial, idle: idle, write: write, peers: make(map[string]net.Conn)}
	f.touch()
	return f
}

func (f *fanout) touch() { f.last.Store(time.Now().UnixNano()) }

// quiet — давно ли не было ни одной датаграммы ни в одну сторону.
func (f *fanout) quiet() bool {
	return time.Since(time.Unix(0, f.last.Load())) >= f.idle
}

// send отправляет датаграмму собеседнику, при первой — открывая к нему
// сокет. Не вышло — датаграмма теряется молча: для UDP это обычное дело, а
// рвать весь поток из-за одного недоступного адреса значило бы уронить игру.
func (f *fanout) send(to vp1.Address, payload []byte) {
	key := to.String()
	f.mu.Lock()
	c := f.peers[key]
	if c == nil && !f.closed && len(f.peers) < maxPeers {
		f.mu.Unlock()
		dialed, err := f.dial(to)
		if err != nil {
			return
		}
		f.mu.Lock()
		if f.closed {
			f.mu.Unlock()
			_ = dialed.Close()
			return
		}
		if c = f.peers[key]; c == nil {
			c = dialed
			f.peers[key] = c
			go f.receive(to, c)
		} else {
			_ = dialed.Close()
		}
	}
	f.mu.Unlock()
	if c == nil {
		return
	}
	f.touch()
	_, _ = c.Write(payload)
}

// receive возвращает клиенту ответы собеседника, подписывая их его адресом.
func (f *fanout) receive(from vp1.Address, c net.Conn) {
	buf := make([]byte, vp1.MaxDatagram)
	for {
		_ = c.SetReadDeadline(time.Now().Add(f.idle))
		n, err := c.Read(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() && !f.quiet() {
				continue
			}
			return
		}
		f.touch()
		f.wmu.Lock()
		err = f.write(from, buf[:n])
		f.wmu.Unlock()
		if err != nil {
			return
		}
	}
}

func (f *fanout) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	for _, c := range f.peers {
		_ = c.Close()
	}
}

// readLoop читает поток клиента, пока он жив; простой с обеих сторон дольше
// idle — конец.
//
// Срок чтения на поток клиента не ставим: истёк бы посреди кадра — и
// следующий разбор начался бы с середины, а поток превратился бы в мусор.
// Вместо этого сторож закрывает поток, когда тишина затянулась.
func readLoop(client net.Conn, f *fanout, next func() error) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		t := time.NewTicker(max(f.idle/4, 10*time.Millisecond))
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				if f.quiet() {
					_ = client.Close()
					return
				}
			}
		}
	}()
	for {
		if err := next(); err != nil {
			if errors.Is(err, io.EOF) || f.quiet() {
				return nil
			}
			return err
		}
	}
}

// ServeXUDP обслуживает XUDP: кадры mux.cool с адресом в каждом пакете.
//
// Кадр — два байта длины метаданных, метаданные (номер сессии, статус,
// флаги, у новой сессии и у пакета с адресом — сеть, порт и адрес) и, если
// есть данные, два байта их длины и сами данные. Ответ — кадр «Keep» с
// адресом того, кто ответил: так Xray узнаёт, от какого сервера пакет.
func ServeXUDP(client net.Conn, dial DialUDP, idle time.Duration) error {
	f := newFanout(dial, idle, func(from vp1.Address, p []byte) error {
		_, err := client.Write(xudpFrame(from, p))
		return err
	})
	defer f.close()

	var (
		dest vp1.Address
		have bool
		head [2]byte
		meta = make([]byte, 512)
		data = make([]byte, vp1.MaxDatagram)
	)
	return readLoop(client, f, func() error {
		if _, err := io.ReadFull(client, head[:]); err != nil {
			return err
		}
		n := int(binary.BigEndian.Uint16(head[:]))
		if n < 4 || n > len(meta) {
			return fmt.Errorf("кадр XUDP: метаданные %d байт", n)
		}
		m := meta[:n]
		if _, err := io.ReadFull(client, m); err != nil {
			return err
		}
		status, opt := m[2], m[3]
		switch status {
		case 1, 2: // новая сессия или продолжение
			if n > 4 {
				if m[4] != 2 {
					return errors.New("кадр XUDP: не UDP")
				}
				to, err := readPortAddr(m[5:])
				if err != nil {
					return err
				}
				dest, have = to, true
			} else if status == 1 {
				return errors.New("кадр XUDP: новая сессия без адреса")
			}
		case 3: // конец
			return io.EOF
		case 4: // «я жив» — данных нет
		default:
			return fmt.Errorf("кадр XUDP: статус %d", status)
		}
		if opt&1 == 0 {
			return nil
		}
		if _, err := io.ReadFull(client, head[:]); err != nil {
			return err
		}
		size := int(binary.BigEndian.Uint16(head[:]))
		if _, err := io.ReadFull(client, data[:size]); err != nil {
			return err
		}
		f.touch()
		if have && status != 4 {
			f.send(dest, data[:size])
		}
		return nil
	})
}

// readPortAddr — порт и адрес в порядке mux.cool: сначала порт, потом тип
// (1 IPv4, 2 домен, 3 IPv6) и сам адрес.
func readPortAddr(b []byte) (vp1.Address, error) {
	if len(b) < 4 {
		return vp1.Address{}, errors.New("кадр XUDP: короткий адрес")
	}
	port := binary.BigEndian.Uint16(b[0:2])
	switch b[2] {
	case 1:
		if len(b) < 7 {
			return vp1.Address{}, errors.New("кадр XUDP: короткий IPv4")
		}
		return vp1.Address{Type: vp1.AtypIPv4, Host: net.IP(b[3:7]).String(), Port: port}, nil
	case 2:
		l := int(b[3])
		if l == 0 || len(b) < 4+l {
			return vp1.Address{}, errors.New("кадр XUDP: короткое имя")
		}
		return vp1.Address{Type: vp1.AtypDomain, Host: string(b[4 : 4+l]), Port: port}, nil
	case 3:
		if len(b) < 19 {
			return vp1.Address{}, errors.New("кадр XUDP: короткий IPv6")
		}
		return vp1.Address{Type: vp1.AtypIPv6, Host: net.IP(b[3:19]).String(), Port: port}, nil
	}
	return vp1.Address{}, fmt.Errorf("кадр XUDP: тип адреса %d", b[2])
}

// xudpFrame — ответ клиенту: «Keep», данные есть, UDP, адрес отправителя.
func xudpFrame(from vp1.Address, p []byte) []byte {
	meta := []byte{0, 0, 2, 1, 2}
	meta = binary.BigEndian.AppendUint16(meta, from.Port)
	switch from.Type {
	case vp1.AtypIPv4:
		meta = append(meta, 1)
		meta = append(meta, net.ParseIP(from.Host).To4()...)
	case vp1.AtypIPv6:
		meta = append(meta, 3)
		meta = append(meta, net.ParseIP(from.Host).To16()...)
	default:
		meta = append(meta, 2, byte(len(from.Host)))
		meta = append(meta, from.Host...)
	}
	out := make([]byte, 0, 4+len(meta)+len(p))
	out = binary.BigEndian.AppendUint16(out, uint16(len(meta)))
	out = append(out, meta...)
	out = binary.BigEndian.AppendUint16(out, uint16(len(p)))
	return append(out, p...)
}

// ServeTrojanUDP обслуживает UDP по Trojan: каждый пакет — адрес как в
// SOCKS5, два байта длины, CRLF и данные. Ответ — в том же виде с адресом
// отправителя.
func ServeTrojanUDP(client net.Conn, dial DialUDP, idle time.Duration) error {
	f := newFanout(dial, idle, func(from vp1.Address, p []byte) error {
		addr, err := from.Marshal()
		if err != nil {
			return err
		}
		out := make([]byte, 0, len(addr)+4+len(p))
		out = append(out, addr...)
		out = binary.BigEndian.AppendUint16(out, uint16(len(p)))
		out = append(out, '\r', '\n')
		_, err = client.Write(append(out, p...))
		return err
	})
	defer f.close()

	var (
		tail [4]byte
		data = make([]byte, vp1.MaxDatagram)
	)
	return readLoop(client, f, func() error {
		to, err := vp1.ReadAddress(client)
		if err != nil {
			return err
		}
		if _, err := io.ReadFull(client, tail[:]); err != nil {
			return err
		}
		if tail[2] != '\r' || tail[3] != '\n' {
			return errors.New("пакет Trojan: нет CRLF")
		}
		size := int(binary.BigEndian.Uint16(tail[:2]))
		if _, err := io.ReadFull(client, data[:size]); err != nil {
			return err
		}
		f.touch()
		f.send(to, data[:size])
		return nil
	})
}
