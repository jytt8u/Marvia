package tunnel

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/jytt8u/marvia/internal/mux"
)

type responseCheck struct {
	done chan struct{}
	err  error
}

// response вызывается под Pool.mu. Незавершённый пинг используется повторно,
// чтобы отменённые проверки не создавали очередь новых горутин.
func (s *pooled) response() *responseCheck {
	if s.check != nil {
		select {
		case <-s.check.done:
			s.check = nil
		default:
		}
	}
	if s.check == nil {
		check := &responseCheck{done: make(chan struct{})}
		s.check = check
		go func() {
			_, check.err = s.sess.Ping()
			if check.err == nil {
				s.activity.mu.Lock()
				s.activity.quiet = time.Now()
				s.activity.mu.Unlock()
			}
			close(check.done)
		}()
	}
	return s.check
}

// Check ждёт ответа каждой установленной сессии. Оценка RTT ядра может
// остаться от старых пакетов; для проверки доступности она не годится.
// Новую сессию здесь не создаём: свежий хендшейк скрывал зависание прежней.
func (p *Pool) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	var checks []*responseCheck
	for _, s := range p.sessions {
		if s.sess.IsClosed() {
			continue
		}
		checks = append(checks, s.response())
	}
	p.mu.Unlock()
	if len(checks) == 0 {
		return errors.New("нет установленного туннеля")
	}
	for _, check := range checks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-check.done:
			if check.err != nil {
				return check.err
			}
		}
	}
	return ctx.Err()
}

type activity struct {
	mu       sync.Mutex
	pending  int
	quiet    time.Time
	received time.Time
}

func (a *activity) begin() {
	a.mu.Lock()
	if a.pending == 0 {
		a.quiet = time.Now()
	}
	a.pending++
	a.mu.Unlock()
}

func (a *activity) end(received int) {
	a.mu.Lock()
	if received > 0 {
		a.quiet = time.Now()
		a.received = a.quiet
	}
	a.pending--
	a.mu.Unlock()
}

// Flowing позволяет отличить контрольный ответ, застрявший в очереди
// медленного TCP, от общего молчания. Прогресс одной сессии не оправдывает
// зависшую загрузку в другой.
func (p *Pool) Flowing(silence time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	flowing := false
	for _, s := range p.sessions {
		s.activity.mu.Lock()
		pending := s.activity.pending > 0
		recent := !s.activity.received.IsZero() && time.Since(s.activity.received) < silence
		s.activity.mu.Unlock()
		if pending {
			if !recent || s.sess.IsClosed() {
				return false
			}
			flowing = true
		}
	}
	return flowing
}

// Stalled — повод проверить ноду, а не доказательство её отказа. Один сайт
// вправе долго не отвечать; рабочая нода ответит на отдельную проверку.
func (p *Pool) Stalled(silence time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.sessions {
		s.activity.mu.Lock()
		stalled := s.activity.pending > 0 && time.Since(s.activity.quiet) >= silence
		s.activity.mu.Unlock()
		if stalled && !s.sess.IsClosed() {
			return true
		}
	}
	return false
}

type observedStream struct {
	net.Conn
	activity *activity
}

func (s *observedStream) Read(b []byte) (int, error) {
	s.activity.begin()
	n, err := s.Conn.Read(b)
	s.activity.end(n)
	return n, err
}

func (s *observedStream) Write(b []byte) (int, error) {
	s.activity.begin()
	n, err := s.Conn.Write(b)
	s.activity.end(0)
	return n, err
}

func (s *observedStream) CloseWrite() error {
	if half, ok := s.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return s.Conn.Close()
}

func openContext(ctx context.Context, session *yamux.Session) (net.Conn, error) {
	type result struct {
		stream net.Conn
		err    error
	}
	done := make(chan result)
	go func() {
		stream, err := mux.Open(session)
		select {
		case done <- result{stream, err}:
		case <-ctx.Done():
			if stream != nil {
				_ = stream.Close()
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		return r.stream, r.err
	}
}

// Quarantine прекращает выдачу потоков до выбора новой ноды. Иначе запросы
// успели бы снова дозвониться к зависшему адресу во время переключения.
func (p *Pool) Quarantine() {
	p.mu.Lock()
	p.unhealthy = true
	sessions := p.sessions
	p.sessions = nil
	p.mu.Unlock()
	for _, s := range sessions {
		_ = s.sess.Close()
	}
}
