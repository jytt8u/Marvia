package client_test

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

// TestReportCarriesOnlyNodeSuccessAndLatency — отчёт приложения о нодах не
// несёт ничего, кроме номера ноды, успеха и задержки.
//
// Замер на устройстве знает куда больше: адрес ноды, текст ошибки, время
// установки соединения, скорость скачивания. Всё это остаётся на телефоне и
// компьютере — продавцу для порядка нод в подписке хватает трёх полей.
func TestReportCarriesOnlyNodeSuccessAndLatency(t *testing.T) {
	var (
		body    []byte
		headers http.Header
	)
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		headers = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer panel.Close()

	measurements := []client.Measurement{
		{
			Node:    client.Node{ID: 3, Name: "fi-1", Country: "Финляндия", Address: "198.51.100.1:443"},
			Latency: 42 * time.Millisecond, RTT: 40 * time.Millisecond,
			Fetch: time.Second, Connect: 30 * time.Millisecond,
		},
		{
			Node: client.Node{ID: 4, Name: "tr-1", Address: "198.51.100.2:443"},
			Err:  errors.New("dial tcp 198.51.100.2:443 from 192.168.1.23: connection refused"),
		},
	}
	if err := client.SendReports(t.Context(), panel.URL+"/sub/токен", client.ReportsFrom(measurements)); err != nil {
		t.Fatal(err)
	}

	var sent map[string][]map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("отчёт не JSON: %v: %s", err, body)
	}
	if len(sent) != 1 || sent["reports"] == nil {
		t.Fatalf("в отчёте лишнее рядом со списком: %s", body)
	}
	for _, report := range sent["reports"] {
		keys := make([]string, 0, len(report))
		for k := range report {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if got := strings.Join(keys, ","); got != "latency_ms,node_id,ok" {
			t.Errorf("в отчёте о ноде поля %s, обещаны только latency_ms,node_id,ok", got)
		}
	}
	for _, leak := range []string{"198.51.100", "192.168.1.23", "fi-1", "Финляндия", "refused"} {
		if strings.Contains(string(body), leak) {
			t.Errorf("в отчёт уехало %q: %s", leak, body)
		}
	}

	// Ни идентификатора устройства, ни версии системы сверх того, что
	// net/http ставит любому запросу.
	for name := range headers {
		switch name {
		case "Content-Type", "Content-Length", "User-Agent", "Accept-Encoding":
		default:
			t.Errorf("в отчёте лишний заголовок %s: %v", name, headers[name])
		}
	}
	if ua := headers.Get("User-Agent"); ua != "" && !strings.HasPrefix(ua, "Go-http-client/") {
		t.Errorf("отчёт представляется приложением с подробностями: %q", ua)
	}
}
