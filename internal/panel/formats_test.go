package panel_test

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func (h *harness) subFormat(token, format string) (int, string, string) {
	h.t.Helper()
	resp, err := h.server.Client().Get(h.server.URL + "/sub/" + token + "?format=" + format)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Content-Type"), string(body)
}

// Приложения sing-box (SFA, SFI, SFM) берут профиль целиком: выход на каждую
// ноду, выбор и DNS через туннель. Наш VP1 они не знают, поэтому в профиле —
// VLESS и Trojan.
func TestSingBoxProfileHasAnOutboundPerNodeAndAccess(t *testing.T) {
	h := newHarness(t)
	h.createNode("fi-1")
	h.createNode("ae-1")
	user := h.createUser(0, "vp1", "vless", "trojan")

	code, kind, body := h.subFormat(user.User.SubToken, "singbox")
	if code != http.StatusOK || !strings.Contains(kind, "json") {
		t.Fatalf("профиль sing-box: код %d, тип %q", code, kind)
	}
	var cfg struct {
		Outbounds []struct {
			Type string `json:"type"`
			Tag  string `json:"tag"`
			UUID string `json:"uuid"`
		} `json:"outbounds"`
		DNS struct {
			Servers []struct {
				Type   string `json:"type"`
				Detour string `json:"detour"`
			} `json:"servers"`
		} `json:"dns"`
	}
	if err := json.Unmarshal([]byte(body), &cfg); err != nil {
		t.Fatalf("профиль не JSON: %v", err)
	}
	kinds := map[string]int{}
	for _, o := range cfg.Outbounds {
		kinds[o.Type]++
	}
	if kinds["vless"] != 2 || kinds["trojan"] != 2 || kinds["selector"] != 1 || kinds["urltest"] != 1 {
		t.Fatalf("выходы в профиле: %v", kinds)
	}
	if cfg.DNS.Servers[0].Type != "https" || cfg.DNS.Servers[0].Detour != "proxy" {
		t.Fatal("имена в профиле спрашиваются не через туннель по HTTPS")
	}
	if strings.Contains(body, "vp1") {
		t.Fatal("в профиль sing-box попал VP1, которого он не знает")
	}
}

func TestClashProfileListsProxiesAndRoutesEverythingThroughThem(t *testing.T) {
	h := newHarness(t)
	h.createNode("fi-1")
	user := h.createUser(0, "vless", "trojan")

	code, kind, body := h.subFormat(user.User.SubToken, "clash")
	if code != http.StatusOK || !strings.Contains(kind, "yaml") {
		t.Fatalf("профиль Clash: код %d, тип %q", code, kind)
	}
	for _, want := range []string{"proxies:", "type: vless", "type: trojan", "proxy-groups:", "MATCH,Marvia", "client-fingerprint:"} {
		if !strings.Contains(body, want) {
			t.Errorf("в профиле Clash нет %q", want)
		}
	}
}

// Метку покупателя пишет продавец, и в ней бывает что угодно. В YAML она не
// должна стать новым ключом или сломать файл.
func TestQuoteInBuyerLabelDoesNotBreakClashProfile(t *testing.T) {
	h := newHarness(t)
	h.createNode("fi-1")
	var out createUserResponse
	if code := h.do(http.MethodPost, "/api/v1/users", adminToken,
		map[string]any{"label": "Вася: \"VIP\"\n  type: direct", "kinds": []string{"vless"}}, &out); code != http.StatusOK {
		t.Fatalf("покупатель: код %d", code)
	}
	_, _, body := h.subFormat(out.User.SubToken, "clash")
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == "type: direct" {
			t.Fatal("метка покупателя вставила в профиль свою строку")
		}
	}
}

func TestProfileWithoutStockAccessSaysWhatIsMissing(t *testing.T) {
	h := newHarness(t)
	h.createNode("fi-1")
	user := h.createUser(0) // только vp1
	code, _, body := h.subFormat(user.User.SubToken, "singbox")
	if code != http.StatusConflict || !strings.Contains(body, "VLESS") {
		t.Fatalf("без VLESS и Trojan: код %d, %q", code, body)
	}
}

// Обычная подписка без format остаётся прежней: Happ, v2RayTun и Hiddify
// получают base64 со ссылками, а не профиль.
func TestPlainSubscriptionIsStillBase64Links(t *testing.T) {
	h := newHarness(t)
	h.createNode("fi-1")
	user := h.createUser(0, "vless")
	resp, err := h.server.Client().Get(h.server.URL + "/sub/" + user.User.SubToken)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "outbounds") || strings.Contains(string(body), "proxies:") {
		t.Fatal("обычная подписка стала профилем")
	}
}
