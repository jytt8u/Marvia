// Package proxyproto передаёт ноде настоящий адрес покупателя, пришедшего
// через вход.
//
// Вход в каскаде перекладывает байты на зарубежную ноду, и та видит одного
// собеседника — сам вход. А нода считает устройства покупателя по адресам:
// без настоящего адреса все покупатели входа слились бы в одно «устройство»,
// и лимит устройств перестал бы значить что-либо. Поэтому вход первым делом
// сообщает, от кого соединение, заголовком PROXY protocol версии 2 — того же
// формата, что у HAProxy и nginx: свой формат для одной строчки адреса ничего
// бы не дал, а чужой хотя бы читается их инструментами при отладке.
//
// Заголовку верят только от адресов своих входов (Listen, trusted): от
// любого другого он был бы способом выдать себя за кого угодно и обойти
// лимит устройств.
package proxyproto

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// signature открывает заголовок версии 2. В ней нарочно перевод строки и
// нулевой байт: ни HTTP, ни TLS так не начинаются, и спутать заголовок с
// данными клиента нельзя.
var signature = []byte{0x0D, 0x0A, 0x0D, 0x0A, 0x00, 0x0D, 0x0A, 0x51, 0x55, 0x49, 0x54, 0x0A}

const (
	cmdLocal = 0x20 // соединение от самого входа: проверка, а не покупатель
	cmdProxy = 0x21

	famTCP4 = 0x11
	famTCP6 = 0x21

	// maxBody — сколько байт адресов и расширений принимаем. Адресам TCP6
	// хватает 36; остальное — запас на расширения, которые мы пропускаем.
	maxBody = 512

	// headerTimeout — сколько вход может молчать, прежде чем сказать адрес.
	// Он пишет заголовок сразу после соединения, так что долгое молчание —
	// признак не входа, а чего-то иного.
	headerTimeout = 5 * time.Second
)

// Header собирает заголовок для соединения от src к dst. Адреса не TCP или
// разных семейств дают заголовок LOCAL: нода тогда оставит адрес входа, а не
// примет мусор за адрес покупателя.
func Header(src, dst net.Addr) []byte {
	s, sok := src.(*net.TCPAddr)
	d, dok := dst.(*net.TCPAddr)
	var buf bytes.Buffer
	buf.Write(signature)
	if !sok || !dok {
		buf.Write([]byte{cmdLocal, 0x00, 0x00, 0x00})
		return buf.Bytes()
	}
	s4, d4 := s.IP.To4(), d.IP.To4()
	switch {
	case s4 != nil && d4 != nil:
		buf.Write([]byte{cmdProxy, famTCP4, 0x00, 12})
		buf.Write(s4)
		buf.Write(d4)
	case s4 == nil && d4 == nil && len(s.IP) == net.IPv6len && len(d.IP) == net.IPv6len:
		buf.Write([]byte{cmdProxy, famTCP6, 0x00, 36})
		buf.Write(s.IP)
		buf.Write(d.IP)
	default:
		buf.Write([]byte{cmdLocal, 0x00, 0x00, 0x00})
		return buf.Bytes()
	}
	_ = binary.Write(&buf, binary.BigEndian, uint16(s.Port))
	_ = binary.Write(&buf, binary.BigEndian, uint16(d.Port))
	return buf.Bytes()
}

// ErrNotProxy — поток начинается не с заголовка.
var ErrNotProxy = errors.New("нет заголовка PROXY")

// Read читает заголовок и отдаёт адрес покупателя. nil без ошибки — заголовок
// LOCAL: соединение от самого входа, адрес остаётся его.
//
// Читается ровно заголовок, ни байтом больше: следом идёт приветствие
// клиента, и его должен получить тот, кто разбирает протокол.
func Read(r io.Reader) (net.Addr, error) {
	var head [16]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, fmt.Errorf("заголовок PROXY: %w", err)
	}
	if !bytes.Equal(head[:12], signature) {
		return nil, ErrNotProxy
	}
	cmd, fam := head[12], head[13]
	size := int(binary.BigEndian.Uint16(head[14:16]))
	if size > maxBody {
		return nil, fmt.Errorf("заголовок PROXY длиной %d", size)
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("заголовок PROXY: %w", err)
	}
	switch cmd {
	case cmdLocal:
		return nil, nil
	case cmdProxy:
	default:
		return nil, fmt.Errorf("заголовок PROXY с командой %#x", cmd)
	}
	switch fam {
	case famTCP4:
		if size < 12 {
			return nil, errors.New("короткий заголовок PROXY")
		}
		return &net.TCPAddr{IP: net.IP(append([]byte(nil), body[:4]...)), Port: int(binary.BigEndian.Uint16(body[8:10]))}, nil
	case famTCP6:
		if size < 36 {
			return nil, errors.New("короткий заголовок PROXY")
		}
		return &net.TCPAddr{IP: net.IP(append([]byte(nil), body[:16]...)), Port: int(binary.BigEndian.Uint16(body[32:34]))}, nil
	}
	// Незнакомое семейство (UDP, unix) — для нас всё равно что LOCAL.
	return nil, nil
}

// Listen оборачивает слушатель: от адресов, которым trusted верит, сначала
// читается заголовок PROXY и подставляется настоящий адрес покупателя;
// остальные соединения проходят как есть.
//
// Заголовок читается не в Accept, а рядом: вход, замолчавший на полпути,
// иначе остановил бы приём всех остальных соединений.
func Listen(inner net.Listener, trusted func(net.IP) bool) net.Listener {
	l := &listener{Listener: inner, trusted: trusted, conns: make(chan accepted), done: make(chan struct{})}
	go l.loop()
	return l
}

type accepted struct {
	conn net.Conn
	err  error
}

type listener struct {
	net.Listener
	trusted func(net.IP) bool
	conns   chan accepted
	done    chan struct{}
	once    sync.Once
}

func (l *listener) loop() {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			if !l.deliver(accepted{err: err}) || errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		addr, _ := c.RemoteAddr().(*net.TCPAddr)
		if addr == nil || l.trusted == nil || !l.trusted(addr.IP) {
			if !l.deliver(accepted{conn: c}) {
				_ = c.Close()
				return
			}
			continue
		}
		go func() {
			_ = c.SetReadDeadline(time.Now().Add(headerTimeout))
			src, err := Read(c)
			_ = c.SetReadDeadline(time.Time{})
			if err != nil {
				// От входа без заголовка нельзя понять, чей это трафик.
				// Пустить — значит посчитать покупателя входом.
				_ = c.Close()
				return
			}
			if src != nil {
				c = &conn{Conn: c, remote: src}
			}
			if !l.deliver(accepted{conn: c}) {
				_ = c.Close()
			}
		}()
	}
}

func (l *listener) deliver(a accepted) bool {
	select {
	case l.conns <- a:
		return true
	case <-l.done:
		return false
	}
}

func (l *listener) Accept() (net.Conn, error) {
	select {
	case a := <-l.conns:
		return a.conn, a.err
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *listener) Close() error {
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

// conn — соединение с подставленным адресом покупателя.
type conn struct {
	net.Conn
	remote net.Addr
}

func (c *conn) RemoteAddr() net.Addr { return c.remote }

// CloseWrite пробрасывается: без него половинное закрытие в relay
// превратилось бы в полное, и ответ после конца запроса терялся бы.
func (c *conn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.Conn.Close()
}
