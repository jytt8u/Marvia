package mobile

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

type subscriptionBackend struct {
	client.Backend
	dial func(context.Context, vp1.Address) (net.Conn, error)
}

func (b subscriptionBackend) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	return b.dial(ctx, target)
}

func TestBackgroundSubscriptionUsesTheTunnelAndKeepsItsReminderAndCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "v2rayNG/1.10.0" {
			t.Error("панель не получила привычный User-Agent")
		}
		w.Header().Set("Subscription-Userinfo", "total=100; download=95")
		_, _ = w.Write([]byte("trojan://test-password@node.example:443#Сервер"))
	}))
	defer server.Close()
	called := 0
	backend := subscriptionBackend{dial: func(ctx context.Context, target vp1.Address) (net.Conn, error) {
		called++
		if target.Host != "panel.invalid" || target.Type != vp1.AtypDomain || target.Port != 80 {
			t.Errorf("имя панели разрешилось вне туннеля: %+v", target)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	tunnel := &Tunnel{dialer: backend, running: true}
	cache := t.TempDir()
	link := "http://panel.invalid/sub/тест"
	raw, err := tunnel.Subscription(link, cache, true)
	if err != nil {
		t.Fatal(err)
	}
	var view SubscriptionView
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatal(err)
	}
	if called != 1 || len(view.Nodes) != 1 || view.Remind != "traffic" || view.Left != 5 || view.Stale {
		t.Fatalf("дозвонов %d, подписка %+v", called, view)
	}
	if tunnel.up.Load() == 0 || tunnel.down.Load() == 0 {
		t.Fatal("запрос подписки не учтён в трафике туннеля")
	}
	// Панель больше не отвечает: старый список и напоминание остаются,
	// но прямого запроса в обход заданного туннеля быть не должно.
	tunnel.dialer = subscriptionBackend{dial: func(context.Context, vp1.Address) (net.Conn, error) {
		return nil, errors.New("туннель недоступен")
	}}
	raw, err = tunnel.Subscription(link, cache, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &view); err != nil || !view.Stale || view.Remind != "traffic" {
		t.Fatalf("не сохранились кэш и напоминание: %s, %v", raw, err)
	}
}

func TestStoppedTunnelDoesNotRefreshSubscriptionDirectly(t *testing.T) {
	if _, err := (&Tunnel{}).Subscription("https://panel.invalid/sub/тест", t.TempDir(), true); err == nil {
		t.Fatal("остановленный туннель разрешил обновление")
	}
}

func TestMarviaSubscriptionUsesTheSuppliedTransportAndRetainsTLS(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "format=json" {
			t.Error("панель не получила запрос JSON")
		}
		_, _ = w.Write([]byte(`{"nodes":[{"id":1,"name":"Нода","address":"node.example:443"}]}`))
	}))
	defer server.Close()
	called := false
	transport := subscriptionTransport(func(ctx context.Context, target vp1.Address) (net.Conn, error) {
		called = true
		if target.Host != "panel.invalid" || target.Port != 443 {
			t.Errorf("неверная цель туннеля: %+v", target)
		}
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	})
	defer transport.CloseIdleConnections()
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	// Сертификат тестового сервера выдан на example.com. Имя панели в
	// запросе другое, и отсутствие проверки TLS превратило бы ошибку в успех.
	link := "marvia://AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA@panel.invalid/sub/тест"
	if _, err := subscription(link, t.TempDir(), true, &http.Client{Transport: transport}); err == nil || !called {
		t.Fatalf("подмена имени панели принята или туннель не вызван: %v, %t", err, called)
	}
	transport.TLSClientConfig.ServerName = "example.com"
	if raw, err := subscription(link, t.TempDir(), true, &http.Client{Transport: transport}); err != nil {
		t.Fatalf("подписка не загрузилась через туннель с доверенным сертификатом: %s, %v", raw, err)
	}
}
