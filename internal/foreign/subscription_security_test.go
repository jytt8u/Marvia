package foreign

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestForeignSubscriptionDoesNotSendTokenOverDowngradedHTTP(t *testing.T) {
	var received atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		_, _ = w.Write([]byte("trojan://password@example.test:443"))
	}))
	defer plain.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/sub/test-token", http.StatusFound)
	}))
	defer server.Close()
	old := http.DefaultClient
	http.DefaultClient = server.Client()
	defer func() { http.DefaultClient = old }()
	if _, _, err := FetchRaw(context.Background(), server.URL+"/sub/test-token"); err == nil {
		t.Error("разрешена потеря TLS")
	}
	if received.Load() != 0 {
		t.Fatal("токен отправлен без шифрования")
	}
}
