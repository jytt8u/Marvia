package nodesync

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// TestCascadeRouteSurvivesARestartWithoutThePanel: вход, перезапущенный при
// лежащей панели, по снимку знает, куда перекладывать, а выход — кому верить
// адрес покупателя. Без этого падение панели гасило бы все входы.
func TestCascadeRouteSurvivesARestartWithoutThePanel(t *testing.T) {
	up := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !up {
			http.Error(w, "нет", http.StatusBadGateway)
			return
		}
		_, _ = w.Write([]byte(`{"users":[],"relay_to":"198.51.100.1:443","relays":["203.0.113.5","мусор"]}`))
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "node.access")

	first := New(srv.URL, "node-token").WithSnapshot(path)
	if _, err := first.FetchUsers(context.Background()); err != nil {
		t.Fatal(err)
	}
	if first.RelayTo() != "198.51.100.1:443" || !first.TrustsRelay(net.ParseIP("203.0.113.5")) {
		t.Fatalf("маршрут не принят: %q", first.RelayTo())
	}
	if first.TrustsRelay(net.ParseIP("203.0.113.6")) {
		t.Fatal("выход верит постороннему адресу")
	}

	up = false
	restarted := New(srv.URL, "node-token").WithSnapshot(path)
	if _, _, offline, err := restarted.Bootstrap(context.Background(), nil); err != nil || !offline {
		t.Fatalf("старт без панели: offline=%v, %v", offline, err)
	}
	if restarted.RelayTo() != "198.51.100.1:443" || !restarted.TrustsRelay(net.ParseIP("203.0.113.5")) {
		t.Fatalf("после перезапуска маршрут потерян: %q", restarted.RelayTo())
	}
}

// TestADisabledEntryStopsForwarding: выключенный в панели вход перестаёт
// перекладывать сразу, а не через сутки по снимку.
func TestADisabledEntryStopsForwarding(t *testing.T) {
	disabled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if disabled {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"error":"нода выключена"}`))
			return
		}
		_, _ = w.Write([]byte(`{"users":[],"relay_to":"198.51.100.1:443"}`))
	}))
	defer srv.Close()
	c := New(srv.URL, "node-token")
	if _, err := c.FetchUsers(context.Background()); err != nil || c.RelayTo() == "" {
		t.Fatalf("первая сверка: %v", err)
	}
	disabled = true
	_, _ = c.FetchUsers(context.Background())
	if c.RelayTo() != "" {
		t.Fatalf("выключенный вход ведёт на %q", c.RelayTo())
	}
}
