package tunnel_test

import (
	"context"
	"github.com/jytt8u/marvia/internal/tunnel"
	"net"
	"testing"
	"time"
)

func TestClosingPoolCancelsPendingDialFromLostNetwork(t *testing.T) {
	started := make(chan struct{})
	finished := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := tunnel.NewPool(func(ctx context.Context) (net.Conn, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}, 1, 8)
	defer p.Close()
	go func() { _, err := p.Open(ctx); finished <- err }()
	<-started
	_ = p.Close()
	select {
	case err := <-finished:
		if err == nil {
			t.Fatal("отменённый дозвон прошёл")
		}
	case <-time.After(time.Second):
		t.Fatal("закрытый пул оставил дозвон ждать тайм-аута старой сети")
	}
}
