package panel_test

import (
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/transport"
)

// Отпечаток, который продавец выбрал в панели, а клиент не знает, — это
// нода, оставшаяся на Chrome, хотя продавец уверен в обратном.
func TestEveryFingerprintThePanelOffersIsKnownToTheClient(t *testing.T) {
	for _, name := range panel.Fingerprints {
		if _, ok := transport.Fingerprint(name); !ok {
			t.Errorf("панель предлагает %q, а клиент его не знает", name)
		}
	}
}

func TestPanelPageOffersTheSameFingerprintsAsTheServer(t *testing.T) {
	page, err := os.ReadFile("web/index.html")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`const FINGERPRINTS = \[([^\]]*)\]`).FindSubmatch(page)
	if m == nil {
		t.Fatal("в странице панели нет списка FINGERPRINTS")
	}
	want := `"` + strings.Join(panel.Fingerprints, `", "`) + `"`
	if string(m[1]) != want {
		t.Fatalf("страница предлагает %s, сервер принимает %s", m[1], want)
	}
}

func TestChosenFingerprintReachesLinksAndSubscription(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("fi-1")
	user := h.createUser(0, "vless")

	if code := h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(node.Node.ID), adminToken,
		map[string]any{"fingerprint": "Firefox"}, nil); code != http.StatusOK {
		t.Fatalf("выбор отпечатка: код %d", code)
	}
	if code := h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(node.Node.ID), adminToken,
		map[string]any{"fingerprint": "netscape"}, nil); code != http.StatusBadRequest {
		t.Errorf("незнакомый отпечаток принят: код %d", code)
	}

	token := user.User.SubToken
	stock := h.fetch("/sub/" + token)
	raw, _ := base64.StdEncoding.DecodeString(stock)
	if !strings.Contains(string(raw), "fp=firefox") {
		t.Errorf("в ссылке для чужих приложений не тот отпечаток: %s", raw)
	}
	if body := h.fetch("/sub/" + token + "?format=json"); !strings.Contains(body, `"fingerprint":"firefox"`) {
		t.Errorf("наш клиент не узнает об отпечатке: %s", body)
	}

	// Пусто — обратно к Chrome.
	h.do(http.MethodPatch, "/api/v1/nodes/"+itoa(node.Node.ID), adminToken, map[string]any{"fingerprint": ""}, nil)
	raw, _ = base64.StdEncoding.DecodeString(h.fetch("/sub/" + token))
	if !strings.Contains(string(raw), "fp=chrome") {
		t.Errorf("после сброса не Chrome: %s", raw)
	}
}

func (h *harness) fetch(path string) string {
	h.t.Helper()
	resp, err := http.Get(h.server.URL + path)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		h.t.Fatalf("%s: код %d", path, resp.StatusCode)
	}
	return string(body)
}
