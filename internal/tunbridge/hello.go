package tunbridge

import (
	"encoding/binary"
	"net"
	"strings"
	"time"

	"github.com/xjasonlyu/tun2socks/v2/core/adapter"
)

// Имя сайта из TLS-приветствия — для правил по сайтам (см. sites.go).
//
// Приветствие читается из соединения приложения до дозвона и потом
// отдаётся дальше байт в байт: ни нода, ни сайт не видят разницы. Разбор
// только читает — ничего в приветствии не меняется, и мост не становится
// участником TLS.

// readHello читает начало соединения, пока не наберётся приветствие с
// именем, не кончится wait или не станет ясно, что это не TLS. Отдаёт всё
// прочитанное — его обязательно надо передать дальше — и имя или пусто.
func readHello(conn net.Conn, wait time.Duration) ([]byte, string) {
	_ = conn.SetReadDeadline(time.Now().Add(wait))
	defer func() { _ = conn.SetReadDeadline(time.Time{}) }()

	buf := make([]byte, 0, 2048)
	chunk := make([]byte, 4096)
	for len(buf) < maxHello {
		n, err := conn.Read(chunk[:min(len(chunk), maxHello-len(buf))])
		buf = append(buf, chunk[:n]...)
		name, done := helloName(buf)
		if done {
			return buf, name
		}
		if err != nil {
			break
		}
	}
	return buf, ""
}

// helloName ищет имя в начале потока. done — дальше читать бессмысленно:
// имя найдено, или это не приветствие TLS, или приветствие без имени.
//
// Приветствие собирается из нескольких записей: средства против DPI режут
// его на записи поменьше, чтобы имя не лежало в одной, и мост должен видеть
// имя так же, как его увидит сайт.
func helloName(p []byte) (string, bool) {
	const (
		recordHeader   = 5
		handshake      = 0x16
		clientHello    = 0x01
		messageHeadLen = 4
	)
	var msg []byte
	for len(p) > 0 {
		if p[0] != handshake {
			return "", true
		}
		if len(p) < recordHeader {
			return "", false
		}
		size := int(binary.BigEndian.Uint16(p[3:5]))
		if len(p) < recordHeader+size {
			// Запись не дочитана, но её начало уже годится: имя стоит в
			// первых сотнях байт, и ждать хвоста ради него незачем.
			msg = append(msg, p[recordHeader:]...)
			break
		}
		msg = append(msg, p[recordHeader:recordHeader+size]...)
		p = p[recordHeader+size:]
	}
	if len(msg) < messageHeadLen {
		return "", false
	}
	if msg[0] != clientHello {
		return "", true
	}
	body := msg[messageHeadLen:]
	want := int(msg[1])<<16 | int(msg[2])<<8 | int(msg[3])
	name, found, complete := serverName(body, want)
	if found {
		return name, true
	}
	return "", complete
}

// serverName разбирает тело ClientHello (RFC 8446, 4.1.2) до расширения
// server_name. complete — тело прочитано целиком, и имени в нём нет.
func serverName(b []byte, want int) (name string, found, complete bool) {
	full := len(b) >= want
	if full {
		b = b[:want]
	}
	i := 2 + 32 // версия и случайные байты
	skip := func(width int) bool {
		if i+width > len(b) {
			return false
		}
		n := 0
		for _, c := range b[i : i+width] {
			n = n<<8 | int(c)
		}
		i += width + n
		return i <= len(b)
	}
	// Сессия, шифры, сжатие, затем длина расширений.
	if !skip(1) || !skip(2) || !skip(1) || i+2 > len(b) {
		return "", false, full
	}
	i += 2
	for i+4 <= len(b) {
		kind := binary.BigEndian.Uint16(b[i : i+2])
		size := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		start := i + 4
		if start+size > len(b) {
			return "", false, full
		}
		if kind == 0x0000 { // server_name
			ext := b[start : start+size]
			// Длина списка, тип имени (0 — имя хоста), длина, само имя.
			if len(ext) < 5 || ext[2] != 0 {
				return "", false, true
			}
			n := int(binary.BigEndian.Uint16(ext[3:5]))
			if 5+n > len(ext) {
				return "", false, true
			}
			host := strings.ToLower(strings.TrimSuffix(string(ext[5:5+n]), "."))
			return host, host != "", true
		}
		i = start + size
	}
	return "", false, full
}

// peekedConn — соединение приложения, у которого начало уже прочитано.
// Прочитанное отдаётся первым, дальше — как было.
type peekedConn struct {
	adapter.TCPConn
	head []byte
}

func (c *peekedConn) Read(p []byte) (int, error) {
	if len(c.head) > 0 {
		n := copy(p, c.head)
		c.head = c.head[n:]
		return n, nil
	}
	return c.TCPConn.Read(p)
}

// CloseWrite пробрасывается явно: обёртка прячет методы, которых нет в
// интерфейсе, а без половинного закрытия relay закроет соединение целиком,
// и ответ, который сайт ещё дописывает, потеряется.
func (c *peekedConn) CloseWrite() error {
	if cw, ok := c.TCPConn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return c.TCPConn.Close()
}
