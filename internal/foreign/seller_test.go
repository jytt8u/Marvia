package foreign

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// foreignPanel — чужая панель, которая отдаёт одну ноду и заданные заголовки.
func foreignPanel(t *testing.T, headers map[string]string) string {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		_, _ = w.Write([]byte("trojan://password@node.example.test:443#node"))
	}))
	t.Cleanup(server.Close)
	old := http.DefaultClient
	http.DefaultClient = server.Client()
	t.Cleanup(func() { http.DefaultClient = old })
	return server.URL + "/sub/test-token"
}

func TestForeignSubscriptionBringsSupportRenewAndAnnouncement(t *testing.T) {
	link := foreignPanel(t, map[string]string{
		"support-url":          "https://t.me/seller_support",
		"profile-web-page-url": "https://shop.example/renew",
		// «Плановые работы» — так пишут 3x-ui и Remnawave.
		"announce": "base64:0J/Qu9Cw0L3QvtCy0YvQtSDRgNCw0LHQvtGC0Ys=",
	})
	cache := filepath.Join(t.TempDir(), "foreign.json")

	for _, refresh := range []bool{true, false} { // из сети, затем из кэша
		sub, _, _, err := Load(link, cache, refresh)
		if err != nil {
			t.Fatalf("подписка: %v", err)
		}
		view := View(sub)
		if view.SupportURL != "https://t.me/seller_support" || view.RenewURL != "https://shop.example/renew" ||
			view.Announce != "Плановые работы" {
			t.Fatalf("refresh=%v: %+v", refresh, sub.Seller)
		}
	}
}

func TestForeignSellerLinksFollowTheSameRules(t *testing.T) {
	link := foreignPanel(t, map[string]string{
		"support-url":          "intent://evil#Intent;end",
		"profile-web-page-url": "http://shop.example/renew",
		"announce":             "Скидка\u202e moc.live",
	})
	sub, _, _, err := Load(link, "", true)
	if err != nil {
		t.Fatalf("подписка: %v", err)
	}
	if sub.Seller.SupportURL != "" || sub.Seller.RenewURL != "" {
		t.Errorf("чужие негодные ссылки приняты: %+v", sub.Seller)
	}
	if sub.Seller.Announce != "Скидка moc.live" {
		t.Errorf("объявление не вычищено: %q", sub.Seller.Announce)
	}
}

func TestTamperedForeignCacheIsCleanedOnLoad(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "foreign.json")
	raw := `{"subscription":"` + subscriptionFingerprint("https://127.0.0.1:1/sub/x") + `","fetched_at":1,"body":"trojan://p@node.example.test:443",` +
		`"seller":{"support_url":"javascript:alert(1)","renew_url":"","announce":"ok"}}`
	if err := os.WriteFile(cache, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	// Кэш протух, панель не отвечает — отдаётся старый кэш, и он тоже
	// обязан пройти чистку.
	sub, _, _, err := Load("https://127.0.0.1:1/sub/x", cache, false)
	if err != nil {
		t.Fatalf("подписка из кэша: %v", err)
	}
	if strings.Contains(sub.Seller.SupportURL, "javascript") {
		t.Fatalf("ссылка из кэша не проверена: %+v", sub.Seller)
	}
}

// Привязанный к подписке кэш читается без похода в сеть, и подправленный руками — тоже
// через чистку: окно отдаст эти ссылки системе.
func TestCachedForeignSubscriptionNeedsNoNetworkAndIsCleaned(t *testing.T) {
	cache := filepath.Join(t.TempDir(), "foreign.json")
	// Неотвечающая панель: Cached обязан не пойти по этому адресу вовсе,
	// иначе он вернул бы ошибку или ждал бы тайм-аут.
	link := "https://127.0.0.1:1/sub/x"
	if _, ok := Cached(link, cache); ok {
		t.Fatal("без файла кэш нашёлся")
	}
	raw := `{"subscription":"` + subscriptionFingerprint(link) + `","fetched_at":1,"body":"trojan://p@node.example.test:443",` +
		`"seller":{"support_url":"javascript:alert(1)","renew_url":"https://shop.example/renew","announce":"ok"}}`
	if err := os.WriteFile(cache, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	sub, ok := Cached(link, cache)
	if !ok {
		t.Fatal("кэш не прочитан")
	}
	if sub.Seller.SupportURL != "" || sub.Seller.RenewURL != "https://shop.example/renew" || sub.Seller.Announce != "ok" {
		t.Fatalf("кэш прочитан не так: %+v", sub.Seller)
	}
}
