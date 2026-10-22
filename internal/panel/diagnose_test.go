package panel_test

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type diagAnswer struct {
	Steps []struct {
		ID    string `json:"id"`
		State string `json:"state"`
		Value string `json:"value"`
	} `json:"steps"`
}

func (d diagAnswer) state(id string) string {
	for _, s := range d.Steps {
		if s.ID == id {
			return s.State
		}
	}
	return ""
}

// TestCheckSaysWhereThePathBreaks: проверка из панели называет шаг, на
// котором рвётся путь: закрытый порт — порт, а TLS после него не
// проверяется; живой TLS с чужим сертификатом — неудачное прикрытие; нода,
// ни разу не сверившаяся с панелью, — сверка.
func TestCheckSaysWhereThePathBreaks(t *testing.T) {
	h := newHarness(t)

	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closedAddr := closed.Addr().String()
	_ = closed.Close()
	dead := h.createNode("dead")
	h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(dead.Node.ID), adminToken, map[string]any{"address": closedAddr}, nil)

	var got diagAnswer
	if code := h.do(http.MethodPost, "/api/v1/nodes/"+itoa(dead.Node.ID)+"/check", adminToken, nil, &got); code != http.StatusOK {
		t.Fatalf("проверка: код %d", code)
	}
	if got.state("tcp") != "fail" || got.state("tls") != "skip" || got.state("sync") != "fail" {
		t.Fatalf("закрытый порт: %+v", got.Steps)
	}

	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer tlsSrv.Close()
	live := h.createNode("live")
	addr := strings.TrimPrefix(tlsSrv.URL, "https://")
	h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(live.Node.ID), adminToken, map[string]any{"address": addr}, nil)
	h.nodeUsers(live.Token)

	got = diagAnswer{}
	h.do(http.MethodPost, "/api/v1/nodes/"+itoa(live.Node.ID)+"/check", adminToken, nil, &got)
	if got.state("tcp") != "ok" || got.state("tls") != "warn" || got.state("sync") != "ok" {
		t.Fatalf("живая нода с чужим сертификатом: %+v", got.Steps)
	}
}
