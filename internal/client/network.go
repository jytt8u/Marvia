package client

import "context"

// NetworkChanged приходит от платформы, а не от таймера. TCP-сессия прежней
// сети не переезжает вместе с телефоном: новый дозвон сохраняет ноду и ключ,
// но закрывает старые потоки. При отсутствии сети не опрашиваем все серверы.
func (s *Supervisor) NetworkChanged(available bool) {
	s.mu.Lock()
	if s.closed || s.dialer == nil {
		s.mu.Unlock()
		return
	}
	old := s.dialer
	fresh, err := NewDialer(old.Node(), s.cfg.Key, s.cfg.Dial)
	if err != nil {
		s.mu.Unlock()
		return
	}
	fresh.sub = old.sub
	if previous := old.measurement.Load(); previous != nil {
		m := *previous
		m.RTT = 0
		m.dialer = nil
		fresh.measurement.Store(&m)
	}
	s.dialer = fresh
	s.offline = !available
	s.fails.Store(0)
	if s.networkCancel != nil {
		s.networkCancel()
	}
	life := s.life
	if life == nil {
		life = context.Background()
	}
	s.networkCtx, s.networkCancel = context.WithCancel(life)
	if s.networkWake == nil {
		s.networkWake = make(chan struct{}, 1)
	}
	select {
	case s.networkWake <- struct{}{}:
	default:
	}
	s.mu.Unlock()
	_ = old.Close()
}
