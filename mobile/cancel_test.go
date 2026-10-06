package mobile

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/tunbridge"
	"github.com/jytt8u/marvia/internal/vp1"
)

func TestCanceledConnectionStopsWaitingForSubscription(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	attempt := NewConnectionAttempt()
	done := make(chan error, 1)
	go func() { _, err := attempt.Connect(server.URL, t.TempDir(), 0, `{"disable_reports":true}`); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("подписка не запрошена")
	}
	attempt.Cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("отмена потерялась: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("отмена ждёт таймаута панели")
	}
}

func TestCanceledAttemptCannotConnectAgain(t *testing.T) {
	attempt := NewConnectionAttempt()
	attempt.Cancel()
	_, err := attempt.Connect("https://panel.example.invalid/sub", "", 0, "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("отменённая попытка запустилась: %v", err)
	}
}

func TestCanceledVP1ConnectionStopsDuringPanelTLSHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var first [1]byte
		if _, err := conn.Read(first[:]); err != nil {
			return
		}
		close(entered)
		<-release
	}()
	// Публичный тестовый ключ: ни аккаунта, ни рабочей панели в проверке нет.
	link := "marvia://YH3odubQhSmWQfCRwteeGN6pehcHq3DDcX_uGCvfl_Q@" + listener.Addr().String() + "/sub/test"
	attempt := NewConnectionAttempt()
	done := make(chan error, 1)
	go func() { _, err := attempt.Connect(link, t.TempDir(), 0, `{"disable_reports":true}`); done <- err }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("панель не получила TLS-запрос")
	}
	attempt.Cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("отмена потерялась: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("отмена ждёт рукопожатия панели")
	}
}

type stalledCloseBackend struct {
	client.Backend
	entered, release chan struct{}
	closes           atomic.Int32
}

func (b *stalledCloseBackend) Close() error {
	b.closes.Add(1)
	close(b.entered)
	<-b.release
	return nil
}
func (b *stalledCloseBackend) DialTarget(context.Context, vp1.Address) (net.Conn, error) {
	return nil, errors.New("тестовая нода недоступна")
}
func (b *stalledCloseBackend) DialDatagrams(context.Context, vp1.Address) (net.Conn, error) {
	return nil, errors.New("тестовая нода недоступна")
}

func TestDisconnectDoesNotWaitForBackendAndStopStillCleansIt(t *testing.T) {
	b := &stalledCloseBackend{entered: make(chan struct{}), release: make(chan struct{})}
	device, peer := net.Pipe()
	defer peer.Close()
	bridge, err := tunbridge.Start(tunbridge.Config{Device: device, Dialer: b, DNS: DefaultDNS})
	if err != nil {
		t.Fatal(err)
	}
	tunnel := &Tunnel{running: true, dialer: b, bridge: bridge}
	tunnel.Disconnect()
	if tunnel.Running() {
		t.Fatal("отключённый туннель остался работающим")
	}
	_ = peer.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := peer.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("интерфейс остался открыт: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- tunnel.Stop() }()
	select {
	case <-b.entered:
	case <-time.After(time.Second):
		t.Fatal("отключение потеряло очистку ядра")
	}
	tunnel.Disconnect()
	if err := tunnel.Stop(); err != nil {
		t.Fatal(err)
	}
	close(b.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if b.closes.Load() != 1 {
		t.Fatal("ядро закрыто повторно")
	}
}
