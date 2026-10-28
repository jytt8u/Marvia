package tunnel_test

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/mux"
	"github.com/jytt8u/marvia/internal/tunnel"
	"github.com/jytt8u/marvia/internal/vp1"
)

type pausedLink struct {
	net.Conn
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (c *pausedLink) resume() { c.once.Do(func() { close(c.release) }) }
func (c *pausedLink) Close() error {
	c.resume()
	return c.Conn.Close()
}
func (c *pausedLink) Write(p []byte) (int, error) {
	if c.armed.CompareAndSwap(true, false) {
		close(c.entered)
		<-c.release
	}
	return c.Conn.Write(p)
}

// Это модель ожидания повторной доставки в упорядоченном транспорте,
// а не радиосеть и не измерение оператора. Один недоставленный участок
// обязан задержать и соседний поток, и пинг, если всё идёт в одном потоке
// TCP/QUIC. Проверяем настоящие VP1 и yamux, не подменяя их таймеры.
func TestPauseInSharedTransportDelaysPingAndOtherStreams(t *testing.T) {
	for _, pause := range []time.Duration{300 * time.Millisecond, 600 * time.Millisecond} {
		t.Run(pause.String(), func(t *testing.T) {
			nodeKey, err := vp1.GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			clientKey, err := vp1.GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			a, b := net.Pipe()
			link := &pausedLink{Conn: a, entered: make(chan struct{}), release: make(chan struct{})}
			t.Cleanup(func() { _ = link.Close(); _ = b.Close() })
			ready := make(chan error, 1)
			go func() {
				conn, _, err := vp1.ServerHandshake(b, nodeKey, vp1.NewReplayGuard(vp1.ClockSkew), vp1.AllowAll)
				if err != nil {
					ready <- err
					return
				}
				session, err := mux.Server(conn)
				ready <- err
				if err != nil {
					_ = conn.Close()
					return
				}
				defer session.Close()
				for {
					stream, err := mux.Accept(session)
					if err != nil {
						return
					}
					go func() { defer stream.Close(); _, _ = io.Copy(stream, stream) }()
				}
			}()
			conn, err := vp1.ClientHandshake(link, clientKey, nodeKey.Public)
			if err != nil {
				t.Fatal(err)
			}
			if err := <-ready; err != nil {
				t.Fatal(err)
			}
			pool := tunnel.NewPool(func(context.Context) (net.Conn, error) { return conn, nil }, 1, 32)
			t.Cleanup(func() { _ = pool.Close() })
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			first, err := pool.Open(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer first.Close()
			other, err := pool.Open(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			_ = first.SetDeadline(time.Now().Add(5 * time.Second))
			_ = other.SetDeadline(time.Now().Add(5 * time.Second))
			if rtt, err := pool.Ping(ctx); err != nil {
				t.Fatal(err)
			} else {
				t.Logf("без паузы: %v", rtt)
			}
			link.armed.Store(true)
			defer link.resume()
			written := make(chan error, 1)
			go func() { _, err := first.Write([]byte("загрузка")); written <- err }()
			select {
			case <-link.entered:
			case <-ctx.Done():
				t.Fatal("загрузка не дошла до провода")
			}
			ping := make(chan error, 1)
			start := time.Now()
			go func() { _, err := pool.Ping(ctx); ping <- err }()
			echo := make(chan error, 1)
			go func() {
				_, err := other.Write([]byte{42})
				if err == nil {
					var buf [1]byte
					_, err = io.ReadFull(other, buf[:])
					if err == nil && buf[0] != 42 {
						err = io.ErrUnexpectedEOF
					}
				}
				echo <- err
			}()
			timer := time.NewTimer(pause)
			defer timer.Stop()
			select {
			case err := <-ping:
				t.Fatalf("пинг обогнал недоставленный участок: %v", err)
			case err := <-echo:
				t.Fatalf("соседний поток обогнал недоставленный участок: %v", err)
			case <-timer.C:
			}
			link.resume()
			for _, done := range []<-chan error{written, ping, echo} {
				select {
				case err := <-done:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal("туннель не восстановился после паузы")
				}
			}
			t.Logf("пауза %v задержала пинг и соседний поток на %v", pause, time.Since(start))
		})
	}
}
