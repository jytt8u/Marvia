package tunnel_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/tunnel"
)

// Нода принимает соединение и ничего не читает: пинг мультиплексора ответа
// не дождётся никогда — так же, как пинг, стоящий в очереди за скачиванием.
// Отклик всё равно должен прийти сразу и настоящий.
func TestPingDoesNotWaitBehindTunnelQueue(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	held := make(chan net.Conn, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			held <- c
		}
	}()
	defer func() {
		select {
		case c := <-held:
			_ = c.Close()
		default:
		}
	}()

	pool := tunnel.NewPool(func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", ln.Addr().String())
	}, 1, 32)
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := pool.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	for i := 0; i < 3; i++ {
		start := time.Now()
		rtt, err := pool.Ping(ctx)
		if err != nil || rtt <= 0 {
			t.Fatalf("замер %d: %v, %v", i+1, rtt, err)
		}
		if took := time.Since(start); took > 500*time.Millisecond {
			t.Fatalf("замер %d ждал %v — стоял в очереди туннеля", i+1, took)
		}
	}
}
