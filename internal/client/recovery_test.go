package client_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

func TestWarmupRequiresResponseFromEstablishedNode(t *testing.T) {
	n := startTestNode(t)
	p := freezeProxy(t, n.info.Address, 0, false)
	n.info.Address = p.Address
	d, err := client.NewDialer(n.info, n.clientKey, n.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.Warmup(ctx); err != nil {
		t.Fatal(err)
	}
	p.armed.Store(true)
	check, stop := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stop()
	if err := d.Warmup(check); err == nil {
		t.Fatal("прогрев объявил молчащую сессию рабочей без ответа ноды")
	}
}

func TestProbeDeadlineCoversStatusGrantAndSample(t *testing.T) {
	for _, phase := range []int32{1, 2, 3} {
		t.Run(map[int32]string{1: "status", 2: "grant", 3: "sample"}[phase], func(t *testing.T) {
			n := startTestNode(t)
			n.probeBlock.Store(phase)
			d, err := client.NewDialer(n.info, n.clientKey, n.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			warm, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := d.Warmup(warm); err != nil {
				t.Fatal(err)
			}
			ctx, stop := context.WithTimeout(warm, 150*time.Millisecond)
			defer stop()
			_, err = d.MeasureFetch(ctx, 1024)
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("чтение %d не завершилось по дедлайну: %v", phase, err)
			}
		})
	}
	n := startTestNode(t)
	n.probeBlock.Store(2)
	d, err := client.NewDialer(n.info, n.clientKey, n.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := d.Granted(ctx, 1024); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("чтение согласованного размера пережило дедлайн: %v", err)
	}
}

func TestWarmupWorksWithNodeWithoutSpeedMeasurement(t *testing.T) {
	n := startTestNode(t)
	n.noProbe.Store(true)
	d, err := client.NewDialer(n.info, n.clientKey, n.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := d.Warmup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := d.MeasureFetch(ctx, 1024); err == nil {
		t.Fatal("стенд старой ноды неожиданно умеет замер")
	}
	if err := d.Warmup(ctx); err != nil {
		t.Fatalf("ответ старого мультиплексора отвергнут: %v", err)
	}
}

func downloadTarget(t *testing.T, handle func(net.Conn)) vp1.Address {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err == nil {
			defer conn.Close()
			handle(conn)
		}
	}()
	host, port := splitHostPort(t, ln.Addr().String())
	return vp1.Address{Type: vp1.AtypIPv4, Host: host, Port: port}
}

func supervisedDownload(t *testing.T, target vp1.Address, size int, timeout time.Duration, rate int64) {
	t.Helper()
	n := startTestNode(t)
	n.info.ID = 1
	if rate > 0 {
		p := freezeProxy(t, n.info.Address, 0, false)
		p.rate.Store(rate)
		n.info.Address = p.Address
	}
	const subURL = "https://panel.example.invalid/sub/live-download"
	cache := filepath.Join(t.TempDir(), "subscription.json")
	if err := client.SaveCache(cache, subURL, client.Subscription{Nodes: []client.Node{n.info}}); err != nil {
		t.Fatal(err)
	}
	var switches, troubles atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	s, _, err := client.Supervise(ctx, client.ConnectConfig{Account: client.Account{SubscriptionURL: subURL}, Key: n.clientKey, Dial: n.opts, CachePath: cache, Prefer: 1}, client.Events{
		OnSwitch:  func(client.Node) { switches.Add(1) },
		OnTrouble: func(string) { troubles.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stream, err := s.DialTarget(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	_ = stream.SetDeadline(time.Now().Add(timeout))
	got := make([]byte, size)
	if read, err := io.ReadFull(stream, got); err != nil {
		t.Fatalf("рабочая загрузка оборвана: получено=%d переездов=%d ошибок=%d соединений=%d: %v", read, switches.Load(), troubles.Load(), n.handshakes.Load(), err)
	}
	if !bytes.Equal(got, bytes.Repeat([]byte{0x5a}, size)) {
		t.Fatal("данные загрузки изменились")
	}
	if switches.Load() != 0 || troubles.Load() != 0 || n.handshakes.Load() != 1 {
		t.Fatalf("живая нода ошибочно переподключена: переездов=%d ошибок=%d соединений=%d", switches.Load(), troubles.Load(), n.handshakes.Load())
	}
}

func TestWaitingForOneTargetKeepsWorkingNode(t *testing.T) {
	stop := make(chan struct{})
	defer close(stop)
	target := downloadTarget(t, func(c net.Conn) {
		select {
		case <-time.After(9 * time.Second):
			_, _ = c.Write([]byte{0x5a})
		case <-stop:
		}
	})
	supervisedDownload(t, target, 1, 15*time.Second, 0)
}

func TestLargeDownloadAt64KiBPerSecondKeepsLiveSession(t *testing.T) {
	if os.Getenv("MARVIA_FREEZE_TEST") != "1" {
		t.Skip("длительный стенд: MARVIA_FREEZE_TEST=1")
	}
	target := downloadTarget(t, func(c net.Conn) {
		_, _ = c.Write(bytes.Repeat([]byte{0x5a}, 4<<20))
	})
	// Ограничен весь внешний TCP, поэтому контрольный ответ стоит в той же
	// очереди, что и загрузка. Ограничить только сайт было бы слабее.
	supervisedDownload(t, target, 4<<20, 95*time.Second, 64<<10)
}

func TestMeasureFetchStopsReadingWhenContextEnds(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(map[bool]string{false: "deadline", true: "cancel"}[cancelEarly], func(t *testing.T) {
			n := startTestNode(t)
			p := freezeProxy(t, n.info.Address, 0, false)
			n.info.Address = p.Address
			d, err := client.NewDialer(n.info, n.clientKey, n.opts)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if err := d.Warmup(ctx); err != nil {
				t.Fatal(err)
			}
			p.armed.Store(true)
			check, stop := context.WithTimeout(ctx, 200*time.Millisecond)
			defer stop()
			done := make(chan error, 1)
			go func() { _, err := d.MeasureFetch(check, 1024); done <- err }()
			if cancelEarly {
				select {
				case <-p.Events:
				case <-ctx.Done():
					t.Fatal("запрос не дошёл до прокси")
				}
				stop()
			}
			select {
			case err := <-done:
				want := context.DeadlineExceeded
				if cancelEarly {
					want = context.Canceled
				}
				if !errors.Is(err, want) {
					t.Fatalf("ошибка отмены потеряна: %v", err)
				}
			case <-time.After(700 * time.Millisecond):
				_ = d.Close()
				<-done
				t.Fatal("чтение ответа пережило отмену проверки")
			}
		})
	}
}
