package tunbridge

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/core/adapter"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Ответы на запросы имён через Names (см. Config.Names).

const (
	// namesTimeout — сколько всего ждём ответа на один вопрос, со всеми
	// запасными резолверами. Дольше системный резолвер уже не слушает.
	namesTimeout = 10 * time.Second

	// namesIdle — сколько держим гнездо приложения без новых вопросов.
	// Резолвер системы шлёт A и AAAA из одного гнезда почти разом, а потом
	// замолкает; держать его полторы минуты, как поток датаграмм, незачем.
	namesIdle = 20 * time.Second
)

// serveNames отвечает на вопросы, пришедшие датаграммами в одно гнездо.
//
// Каждый вопрос — в своей горутине: A и AAAA приходят разом, и ждать ответа
// на первый, прежде чем задать второй, значит удвоить время до открытия
// страницы.
func (h *handler) serveNames(conn adapter.UDPConn, first []byte, from net.Addr) {
	var (
		wg      sync.WaitGroup
		writeMu sync.Mutex
	)
	answer := func(query []byte) {
		defer wg.Done()
		reply, err := h.lookup(query, true)
		if err != nil {
			// Молчим, а не отвечаем «ошибка сервера»: такой ответ система
			// запомнила бы и не спрашивала снова, а молчание она переспросит —
			// к тому времени ответит запасной резолвер.
			h.failDial(fmt.Errorf("запрос имени: %w", err))
			return
		}
		writeMu.Lock()
		_, _ = conn.WriteTo(reply, from)
		writeMu.Unlock()
	}

	wg.Add(1)
	go answer(append([]byte(nil), first...))

	b := vp1.DatagramBuffer()
	defer vp1.PutDatagramBuffer(b)
	buf := (*b)[:vp1.MaxDatagram]
	for {
		_ = conn.SetReadDeadline(time.Now().Add(namesIdle))
		n, _, err := conn.ReadFrom(buf)
		if n > 0 {
			wg.Add(1)
			go answer(append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			break
		}
	}
	wg.Wait()
}

// serveNamesTCP отвечает на вопросы, пришедшие по TCP: два байта длины,
// потом сообщение (RFC 1035, 4.2.2). Вопросы в одном соединении идут по
// очереди — так их задаёт почти любой клиент.
func (h *handler) serveNamesTCP(conn net.Conn) {
	for {
		_ = conn.SetReadDeadline(time.Now().Add(namesIdle))
		query, err := readFramed(conn)
		if err != nil {
			return
		}
		reply, err := h.lookup(query, false)
		if err != nil {
			h.failDial(fmt.Errorf("запрос имени по TCP: %w", err))
			return
		}
		if err := writeFramed(conn, reply); err != nil {
			return
		}
	}
}

// lookup — ответ на один вопрос: свой, если IPv6 выключен и спрашивают
// AAAA, иначе — от Names.
func (h *handler) lookup(query []byte, udp bool) ([]byte, error) {
	if reply, ok := h.v6.localAnswer(query); ok {
		return reply, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), namesTimeout)
	defer cancel()
	return h.names.Exchange(ctx, query, udp)
}

// DialAddr — дозвон до адреса через туннель в том виде, в каком его ждёт
// резолвер (securedns.Config.Dial).
//
// Отдавать сюда надо дозвон туннеля, а не развилку Split: соединение с
// резолвером мимо туннеля — это DoH, видный цензору, а его блокируют.
func DialAddr(d Dialer) func(ctx context.Context, addr netip.AddrPort) (net.Conn, error) {
	return func(ctx context.Context, addr netip.AddrPort) (net.Conn, error) {
		return d.DialTarget(ctx, targetOf(addr.Addr().String(), addr.Port()))
	}
}
