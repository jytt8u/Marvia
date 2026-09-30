package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

func TestBrokenRedirectDoesNotExposeSubscriptionToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/sub/"+testToken+"/%zz")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()
	_, err := FetchSubscription(context.Background(), server.URL+"/sub/"+testToken, nil)
	if err == nil {
		t.Fatal("неверное перенаправление принято")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatal("токен подписки попал в текст ошибки перенаправления")
	}
}

func TestNestedURLErrorDoesNotExposeAccessKey(t *testing.T) {
	cause := errors.New("соединение прервано")
	err := &url.Error{Op: "Get", URL: "https://panel.example/sub/" + testToken,
		Err: &url.Error{Op: "parse", URL: "marvia://" + testKey + "@panel.example/sub/" + testToken, Err: cause}}
	for _, clean := range []error{withoutSecret(err), withoutLink(err)} {
		if strings.Contains(clean.Error(), testToken) || strings.Contains(clean.Error(), testKey) {
			t.Error("вложенная ошибка раскрыла ссылку доступа")
		}
		if !errors.Is(clean, cause) {
			t.Error("потеряна причина ошибки")
		}
	}
}

func TestSubscriptionNeverSendsTokenAfterHTTPSDowngrade(t *testing.T) {
	var received atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		_, _ = w.Write([]byte(`{"nodes":[{"id":1}]}`))
	}))
	defer plain.Close()
	tlsServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/sub/"+testToken, http.StatusFound)
	}))
	defer tlsServer.Close()
	oldClient, oldTransport := http.DefaultClient, http.DefaultTransport
	http.DefaultClient = tlsServer.Client()
	http.DefaultTransport = tlsServer.Client().Transport
	defer func() { http.DefaultClient, http.DefaultTransport = oldClient, oldTransport }()
	for _, pins := range [][]netip.Addr{nil, {netip.MustParseAddr("127.0.0.1")}} {
		for _, bypass := range []bool{false, true} {
			var err error
			if bypass {
				_, err = FetchBypass(context.Background(), tlsServer.URL+"/sub/"+testToken, pins)
			} else {
				_, err = FetchSubscription(context.Background(), tlsServer.URL+"/sub/"+testToken, pins)
			}
			if err == nil {
				t.Error("разрешено перенаправление HTTPS на HTTP")
			}
		}
	}
	if received.Load() != 0 {
		t.Fatalf("токен ушёл по незашифрованному HTTP: %d запросов", received.Load())
	}
}
