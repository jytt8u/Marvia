package panel_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/seller"
)

func TestSellerLinkOtherThanHTTPSOrTelegramIsRefused(t *testing.T) {
	h := newHarness(t)
	for _, body := range []map[string]any{
		{"support_url": "http://t.me/support"},
		{"renew_url": "javascript:alert(1)"},
		{"renew_url": "file:///etc/passwd"},
		{"announce": strings.Repeat("я", seller.MaxAnnounce+1)},
	} {
		if code := h.do(http.MethodPut, "/api/v1/seller", adminToken, body, nil); code != http.StatusBadRequest {
			t.Errorf("%v: ожидался 400, получен %d", body, code)
		}
	}

	var got seller.Info
	if code := h.do(http.MethodGet, "/api/v1/seller", adminToken, nil, &got); code != http.StatusOK {
		t.Fatalf("чтение: %d", code)
	}
	if !got.Empty() {
		t.Fatalf("отвергнутое всё равно сохранилось: %+v", got)
	}
}

func TestBotKeyReadsSellerLinksButCannotChangeThem(t *testing.T) {
	h := newHarness(t)
	want := seller.Info{RenewURL: "https://t.me/seller_bot?start=renew"}
	if code := h.do(http.MethodPut, "/api/v1/seller", adminToken, want, nil); code != http.StatusOK {
		t.Fatalf("сохранение админом: %d", code)
	}

	var issued struct {
		Secret string `json:"secret"`
	}
	h.do(http.MethodPost, "/api/v1/keys", adminToken, map[string]any{"name": "бот", "scopes": []string{"read"}}, &issued)

	var got seller.Info
	if code := h.do(http.MethodGet, "/api/v1/seller", issued.Secret, nil, &got); code != http.StatusOK || got != want {
		t.Fatalf("ключ с правом read: код %d, прочитал %+v", code, got)
	}
	if code := h.do(http.MethodPut, "/api/v1/seller", issued.Secret, seller.Info{}, nil); code != http.StatusForbidden {
		t.Fatalf("ключ бота поменял ссылки: код %d", code)
	}
}

func TestSellerChangeLandsInJournalWithoutTheLinks(t *testing.T) {
	h := newHarness(t)
	h.do(http.MethodPut, "/api/v1/seller", adminToken, seller.Info{
		SupportURL: "tg://resolve?domain=seller_support",
		Announce:   "Работы ночью",
	}, nil)
	// Повтор того же самого — не событие.
	h.do(http.MethodPut, "/api/v1/seller", adminToken, seller.Info{
		SupportURL: "tg://resolve?domain=seller_support",
		Announce:   "Работы ночью",
	}, nil)

	var out struct {
		Events []panel.Event `json:"events"`
	}
	h.do(http.MethodGet, "/api/v1/events", adminToken, nil, &out)
	var found []panel.Event
	for _, e := range out.Events {
		if e.Action == panel.EventSellerUpdate {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("записей о правке %d, ожидалась одна", len(found))
	}
	if strings.Contains(found[0].Detail, "seller_support") || !strings.Contains(found[0].Detail, "ссылка поддержки задана") {
		t.Fatalf("пояснение в журнале: %q", found[0].Detail)
	}
}

func TestSubscriptionGivesForeignAppsSupportRenewAndAnnouncement(t *testing.T) {
	h := newHarness(t)
	h.createNode("msk")
	user := h.createUser(0, panel.CredVLESS)
	info := seller.Info{
		SupportURL: "tg://resolve?domain=seller_support",
		RenewURL:   "https://shop.example.com/renew",
		Announce:   "Профилактика с 2:00 до 4:00",
	}
	if code := h.do(http.MethodPut, "/api/v1/seller", adminToken, info, nil); code != http.StatusOK {
		t.Fatalf("сохранение: %d", code)
	}

	resp, err := h.server.Client().Get(h.server.URL + "/sub/" + user.User.SubToken)
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("support-url"); got != info.SupportURL {
		t.Errorf("support-url = %q", got)
	}
	if got := resp.Header.Get("profile-web-page-url"); got != info.RenewURL {
		t.Errorf("profile-web-page-url = %q", got)
	}
	if got := resp.Header.Get("announce"); got != seller.Encode(info.Announce) {
		t.Errorf("announce = %q", got)
	}
	if got := resp.Header.Get("profile-title"); got != seller.Encode("заказ 1043") {
		t.Errorf("profile-title = %q", got)
	}

	resp, err = h.server.Client().Get(h.server.URL + "/sub/" + user.User.SubToken + "?format=json")
	if err != nil {
		t.Fatalf("подписка json: %v", err)
	}
	defer resp.Body.Close()
	var body seller.Info
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("разбор: %v", err)
	}
	if body != info {
		t.Fatalf("в JSON %+v, ожидалось %+v", body, info)
	}
}

func TestSubscriptionWithoutSellerSettingsHasNoButtonsHeaders(t *testing.T) {
	h := newHarness(t)
	h.createNode("msk")
	user := h.createUser(0, panel.CredVLESS)

	resp, err := h.server.Client().Get(h.server.URL + "/sub/" + user.User.SubToken)
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	resp.Body.Close()
	for _, name := range []string{"support-url", "profile-web-page-url", "announce"} {
		if v := resp.Header.Get(name); v != "" {
			t.Errorf("%s ушёл пустым продавцом: %q", name, v)
		}
	}
}
