package client

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/tunnel"
)

func TestFailedPingDoesNotKeepResponseFromPreviousNetwork(t *testing.T) {
	networkErr := errors.New("сеть недоступна")
	d := &Dialer{node: Node{ID: 2, Name: "тестовая нода"}}
	d.pool = tunnel.NewPool(func(context.Context) (net.Conn, error) { return nil, networkErr }, 1, 8)
	t.Cleanup(func() { _ = d.pool.Close() })
	d.measurement.Store(&Measurement{Node: d.node, RTT: 27 * time.Millisecond, Fetch: time.Second, Connect: 90 * time.Millisecond})
	s := &Supervisor{dialer: d}
	if _, err := s.Ping(context.Background()); err == nil {
		t.Fatalf("потеря сети не передана: %v", err)
	}
	m := s.Measurement()
	if m.PingMS() != 0 {
		t.Fatalf("после потери сети показан прежний отклик: %d мс", m.PingMS())
	}
	if m.Node.ID != 2 || m.Fetch != time.Second || m.Connect != 90*time.Millisecond {
		t.Fatalf("вместе с откликом потеряны отдельные результаты подключения: %+v", m)
	}
}
