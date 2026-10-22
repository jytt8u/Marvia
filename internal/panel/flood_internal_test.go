package panel

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// TestRequestsBeyondTheConcurrencyLimitGetAQuick503 — сверх предела запросов
// в работе панель отвечает 503 сразу, не дожидаясь, пока освободится место,
// а освободившееся место снова обслуживает.
func TestRequestsBeyondTheConcurrencyLimitGetAQuick503(t *testing.T) {
	release := make(chan struct{})
	var inside sync.WaitGroup
	inside.Add(2)
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/slow" {
			inside.Done()
			<-release
		}
		w.WriteHeader(http.StatusOK)
	})
	g := newFloodGuard(slow, floodLimits{concurrent: 2}, nil)

	do := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, req)
		return rec
	}

	var done sync.WaitGroup
	for range 2 {
		done.Add(1)
		go func() { defer done.Done(); do("/slow") }()
	}
	inside.Wait()

	started := time.Now()
	rec := do("/fast")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("третий запрос при пределе два получил %d, ждали 503", rec.Code)
	}
	if took := time.Since(started); took > time.Second {
		t.Fatalf("отказ занял %v: он должен быть мгновенным", took)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("503 без Retry-After")
	}

	close(release)
	done.Wait()
	if code := do("/fast").Code; code != http.StatusOK {
		t.Fatalf("после освобождения места ответ %d", code)
	}
}

// TestAddressTableOfThePanelStaysBounded — сколько бы адресов ни пришло,
// панель помнит не больше предела, а старые записи забываются.
func TestAddressTableOfThePanelStaysBounded(t *testing.T) {
	at := time.Unix(1_700_000_000, 0)
	limits := productionFlood
	limits.tracked = 50
	g := newFloodGuard(http.NotFoundHandler(), limits, func() time.Time { return at })

	for i := range 500 {
		req := httptest.NewRequest(http.MethodGet, "/sub/x", nil)
		req.RemoteAddr = fmt.Sprintf("198.51.%d.%d:40000", 100+i/250, i%250+1)
		rec := httptest.NewRecorder()
		g.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("адрес %d получил %d: не уместившийся в таблицу должен проходить", i, rec.Code)
		}
	}
	if n := g.trackedCount(); n > 50 {
		t.Fatalf("в памяти %d адресов при пределе 50", n)
	}

	// Неудачи за окном и полные вёдра забываются при чистке.
	at = at.Add(limits.window + time.Minute)
	if _, ok := g.admit(mustKey(t, g, "192.0.2.10")); !ok {
		t.Fatal("новый адрес не пропущен после чистки")
	}
	if n := g.trackedCount(); n != 1 {
		t.Fatalf("после чистки в памяти %d адресов, ждали один новый", n)
	}
}

func mustKey(t *testing.T, g *floodGuard, ip string) netip.Addr {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = ip + ":40000"
	key, ok := g.keyOf(req)
	if !ok {
		t.Fatalf("адрес %s не учитывается", ip)
	}
	return key
}
