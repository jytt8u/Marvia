package client

import (
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Ранний старт потока.
//
// Раньше каждое соединение приложения отправляло ноде запрос цели и ждало её
// ответа — «дозвонилась до сайта» или «нет», — и только потом пускало первый
// байт данных. Это лишний круг до ноды на каждое соединение: страница из
// двадцати запросов при пинге 70 мс до ноды открывалась заметно медленнее,
// чем через VLESS, который шлёт заголовок и данные сразу.
//
// Теперь поток отдаётся сразу после запроса. Данные приложения едут вслед за
// ним, а ответ ноды читается перед первыми байтами ответа сайта. Нода от
// этого не меняется — и старая тоже: ранние данные ждут в буфере потока,
// пока она дозванивается, и читаются после.
//
// Чем платим: отказ ноды по цели приходит не из DialTarget, а первым чтением.
// Тот, кто по отказу принимал решения — сторож переезда, учёт IPv6, журнал, —
// подписывается на него через OnStatus.

// statusTimeout — сколько ждём ответа ноды при первом чтении. Столько же,
// сколько нода сама даёт себе на дозвон до сайта, с запасом на дорогу.
const statusTimeout = 25 * time.Second

// earlyConn — поток, ответ ноды на который ещё не прочитан.
type earlyConn struct {
	net.Conn
	target vp1.Address

	once     sync.Once
	err      error
	mu       sync.Mutex
	done     bool
	watchers []func(error)
}

func newEarlyConn(stream net.Conn, target vp1.Address) *earlyConn {
	return &earlyConn{Conn: stream, target: target}
}

// OnStatus сообщает fn ответ ноды: nil — пустила, иначе ошибка. Если ответ
// уже прочитан, fn вызывается сразу.
func (c *earlyConn) OnStatus(fn func(error)) {
	c.mu.Lock()
	if c.done {
		err := c.err
		c.mu.Unlock()
		fn(err)
		return
	}
	c.watchers = append(c.watchers, fn)
	c.mu.Unlock()
}

func (c *earlyConn) status() error {
	c.once.Do(func() {
		_ = c.Conn.SetReadDeadline(time.Now().Add(statusTimeout))
		code, err := vp1.ReadStatus(c.Conn)
		_ = c.Conn.SetReadDeadline(time.Time{})
		switch {
		case err != nil:
			c.err = fmt.Errorf("%w: ответ ноды по %s: %w", vp1.ErrNodeUnreachable, c.target, err)
		case code != vp1.StatusOK:
			c.err = &vp1.RefusedError{Target: c.target.String(), Status: code}
		}
		c.mu.Lock()
		c.done = true
		watchers := c.watchers
		c.watchers = nil
		c.mu.Unlock()
		for _, fn := range watchers {
			fn(c.err)
		}
	})
	return c.err
}

func (c *earlyConn) Read(p []byte) (int, error) {
	if err := c.status(); err != nil {
		return 0, err
	}
	return c.Conn.Read(p)
}

// CloseWrite передаёт полузакрытие дальше: без него сайт не узнал бы, что
// приложение досказало запрос, и ответа ждали бы обе стороны.
func (c *earlyConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	// Как у остальных обёрток потока: без полузакрытия — закрыть целиком.
	return c.Conn.Close()
}
