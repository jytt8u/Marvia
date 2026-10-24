package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

// TestSlowNodeIsReportedWithItsFetchTime: в отчёт уходит, за сколько нода
// отдала пробную порцию, — по нему панель видит, что ноду режут.
func TestSlowNodeIsReportedWithItsFetchTime(t *testing.T) {
	got := client.ReportsFrom([]client.Measurement{{Node: client.Node{ID: 3}, Latency: 40 * time.Millisecond, Fetch: 6 * time.Second}})
	if len(got) != 1 || got[0].FetchMS != 6000 {
		t.Fatalf("отчёт %+v", got)
	}
}

// TestOldPanelStillGetsTheReportWithoutTheNewField: панель старше поля
// fetch_ms отвечает 400 на незнакомое поле — клиент шлёт отчёт ещё раз без
// него, и «ответила или нет» до панели всё равно доходит.
func TestOldPanelStillGetsTheReportWithoutTheNewField(t *testing.T) {
	var accepted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reports []struct {
				NodeID    int64 `json:"node_id"`
				OK        bool  `json:"ok"`
				LatencyMS int64 `json:"latency_ms"`
			} `json:"reports"`
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		accepted = len(body.Reports)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	err := client.SendReports(context.Background(), srv.URL, []client.Report{{NodeID: 3, OK: true, LatencyMS: 40, FetchMS: 6000}})
	if err != nil || accepted != 1 {
		t.Fatalf("старая панель: принято %d, ошибка %v", accepted, err)
	}
}
