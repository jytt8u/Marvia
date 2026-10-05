package client_test

import (
	"context"
	"io"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

func TestRestoredNetworkReconnectsChosenNodeBeforePeriodicProbe(t *testing.T) {
	n := startTestNode(t)
	n.info.ID = 7
	const subURL = "https://panel.example.invalid/sub/test"
	cache := filepath.Join(t.TempDir(), "subscription.json")
	if err := client.SaveCache(cache, subURL, client.Subscription{Nodes: []client.Node{n.info}}); err != nil {
		t.Fatal(err)
	}
	recovered := make(chan struct{}, 4)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _, err := client.Supervise(ctx, client.ConnectConfig{Account: client.Account{SubscriptionURL: subURL}, Key: n.clientKey, Dial: n.opts, CachePath: cache, Prefer: 7}, client.Events{OnRecovered: func() { recovered <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 3; i++ {
		s.NetworkChanged(false)
		if s.Measurement().RTT != 0 {
			t.Fatal("потеря сети оставила прежний пинг")
		}
		if _, err := s.Ping(ctx); err == nil {
			t.Fatal("отключённая сеть объявлена доступной")
		}
		s.NetworkChanged(true)
		select {
		case <-recovered:
		case <-time.After(3 * time.Second):
			t.Fatal("восстановление ждёт планового замера вместо сигнала сети")
		}
		if s.Node().ID != 7 || s.Selected() != 7 {
			t.Fatal("переподключение сменило выбранную ноду")
		}
		if _, err := s.Ping(ctx); err != nil {
			t.Fatalf("восстановленная сессия не отвечает: %v", err)
		}
	}
	start := time.Now()
	_ = s.Close()
	s.NetworkChanged(true)
	if time.Since(start) > time.Second {
		t.Fatal("позднее событие сети задержало остановку")
	}
}

func TestMeasurementFromPreviousNetworkCannotRestoreOldPing(t *testing.T) {
	n := startTestNode(t)
	n.info.ID = 7
	upstream := n.info.Address
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var pause atomic.Bool
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	defer close(release)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				if pause.Load() {
					started <- struct{}{}
					<-release
				}
				remote, err := net.DialTimeout("tcp", upstream, time.Second)
				if err != nil {
					return
				}
				defer remote.Close()
				go func() { _, _ = io.Copy(remote, conn) }()
				_, _ = io.Copy(conn, remote)
			}()
		}
	}()
	other := n.info
	other.ID = 8
	other.Address = ln.Addr().String()
	const subURL = "https://panel.example.invalid/sub/network-measurement"
	cache := filepath.Join(t.TempDir(), "subscription.json")
	if err := client.SaveCache(cache, subURL, client.Subscription{Nodes: []client.Node{n.info, other}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, _, err := client.Supervise(ctx, client.ConnectConfig{Account: client.Account{SubscriptionURL: subURL}, Key: n.clientKey, Dial: n.opts, CachePath: cache, Prefer: 7}, client.Events{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	pause.Store(true)
	finished := make(chan []client.Measurement, 1)
	go func() { finished <- s.Measure(ctx) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("замер не начал соединение")
	}
	s.NetworkChanged(false)
	release <- struct{}{}
	select {
	case results := <-finished:
		if s.Measurement().RTT != 0 || len(results) != 0 {
			t.Fatal("замер прежней сети вернул устаревший отклик после потери сети")
		}
	case <-ctx.Done():
		t.Fatal("смена сети не отменила прежний замер")
	}
}
