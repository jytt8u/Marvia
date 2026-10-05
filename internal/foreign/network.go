package foreign

import (
	"context"
)

// NetworkChanged сбрасывает транспорт Xray той же ноды: QUIC и TCP нельзя
// оставлять привязанными к исчезнувшей сети до следующего таймера.
func (s *Supervisor) NetworkChanged(available bool) {
	s.mu.Lock()
	if s.closed || s.engine == nil {
		s.mu.Unlock()
		return
	}
	fresh, err := StartWith(s.engine.Link(), s.opts)
	if err != nil {
		s.mu.Unlock()
		return
	}
	old := s.engine
	s.engine = fresh
	s.offline = !available
	s.networkCancel()
	s.networkCtx, s.networkCancel = context.WithCancel(s.life)
	m := s.measured[s.current.ID]
	m.Node = s.current
	m.RTT = 0
	m.Err = nil
	s.measured[s.current.ID] = m
	select {
	case s.networkWake <- struct{}{}:
	default:
	}
	s.mu.Unlock()
	_ = old.Close()
}

func (s *Supervisor) network() (context.Context, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.networkCtx, s.offline
}
