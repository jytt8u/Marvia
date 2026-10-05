package panel_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/seller"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"golang.org/x/net/html"
)

const browserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"

func buyerRequest(t *testing.T, h *harness, path, agent, accept, language string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, h.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("User-Agent", agent)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", language)
	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, string(raw)
}

// Браузер получает страницу, а Happ — прежнюю подписку даже при Accept: text/html.
func TestBrowserGetsAPageAndHappGetsASubscription(t *testing.T) {
	h := newHarness(t)
	h.createNode("nl")
	created := h.createUser(1024, "vless")
	path := "/sub/" + created.User.SubToken
	resp, page := buyerRequest(t, h, path, browserAgent, "text/html,application/xhtml+xml,*/*;q=0.8", "ru")
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") || !strings.HasPrefix(page, "<!DOCTYPE html>") {
		t.Fatalf("браузер не получил страницу: код %d, тип %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, agent := range []string{"Happ/3.22.0 (Android)", "Happ/1.2.0 (iPhone; iOS 17.5)", browserAgent + " Happ/3.22.0"} {
		resp, body := buyerRequest(t, h, path, agent, "text/html", "")
		raw, err := base64.StdEncoding.DecodeString(body)
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" || err != nil || !strings.Contains(string(raw), "vless://") {
			t.Errorf("Happ получил не подписку: %s, код %d, тип %s", agent, resp.StatusCode, resp.Header.Get("Content-Type"))
		}
	}
}

func TestDesktopAndPhoneBrowsersGetTheBuyerPage(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0)
	for _, agent := range []string{
		browserAgent,
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:128.0) Gecko/20100101 Firefox/128.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) FxiOS/130.0 Mobile/15E148 Safari/605.1.15",
		"Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Mobile Safari/537.36",
	} {
		resp, page := buyerRequest(t, h, "/sub/"+created.User.SubToken, agent, "text/html;q=1,application/xhtml+xml,*/*;q=0.8", "ru")
		if resp.StatusCode != http.StatusOK || !strings.HasPrefix(page, "<!DOCTYPE html>") || !strings.Contains(page, "Без ограничения по трафику") {
			t.Errorf("браузер не получил страницу безлимитного доступа: %q", agent)
		}
	}
}

// Чужое объявление не может вставить разметку.
func TestAnotherSellersAnnouncementCannotInsertMarkup(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0)
	announce := `<img src="https://evil.example/leak" onerror="alert(1)"><script>alert(2)</script>`
	if code := h.do(http.MethodPut, "/api/v1/seller", adminToken, map[string]any{"announce": announce}, nil); code != http.StatusOK {
		t.Fatalf("настройки продавца: %d", code)
	}
	_, page := buyerRequest(t, h, "/sub/"+created.User.SubToken, browserAgent, "text/html", "ru")
	if strings.Contains(page, announce) || !strings.Contains(page, "&lt;img") || !strings.Contains(page, "&lt;script&gt;") {
		t.Fatal("объявление не показано безопасным текстом")
	}
}

// Hiddify называет себя «like ClashMeta v2ray sing-box» в своём исходнике:
// https://github.com/hiddify/hiddify-app/blob/88d2449d2a3379a4d2f9fc1c83285229931b8176/lib/core/model/app_info_entity.dart
// Его Dio не добавляет Accept: text/html:
// https://github.com/hiddify/hiddify-app/blob/88d2449d2a3379a4d2f9fc1c83285229931b8176/lib/core/http_client/dio_http_client.dart
// Happ/Android и Happ/iPhone — запросы из тестов обработчика подписок 3x-ui:
// https://github.com/MHSanaei/3x-ui/blob/d1b60799ecd46616c2c62afc8e166b1480e7bc84/internal/sub/happ_test.go
// У закрытых клиентов нельзя привязать поведение к одному Accept. Поэтому
// проверяем также явный HTML и браузерную приставку перед именем приложения.
func TestSubscriptionAppsNeverReceiveHTML(t *testing.T) {
	h := newHarness(t)
	h.createNode("nl")
	created := h.createUser(4096, panel.CredVLESS, panel.CredTrojan)
	path := "/sub/" + created.User.SubToken
	_, before := buyerRequest(t, h, path, "Go-http-client/1.1", "", "")
	for _, agent := range []string{
		"Happ/3.22.0 (Android)", "Happ/1.2.0 (iPhone; iOS 17.5)",
		"v2raytun/1.0/ios", "V2RayTun/2.3.1 CFNetwork/1568 Darwin/24.1.0",
		"HiddifyNext/2.5.7 (android) like ClashMeta v2ray sing-box", "HiddifyNextX/2.5.7 (windows) like ClashMeta v2ray sing-box",
		"Hiddify/2.0", "Marvia/0.13.0", "Go-http-client/1.1", "curl/8.7.1", "Dart/3.3 (dart:io)", "okhttp/4.12.0", "", "неизвестное приложение",
	} {
		t.Run(agent, func(t *testing.T) {
			for _, accept := range []string{"", "*/*", "application/json", "text/html", "text/html,application/xhtml+xml,*/*;q=0.8"} {
				for _, ua := range []string{agent, browserAgent + " " + agent} {
					if agent == "" || agent == "неизвестное приложение" || strings.HasPrefix(agent, "Dart/") || strings.HasPrefix(agent, "okhttp/") {
						ua = agent
					}
					resp, body := buyerRequest(t, h, path, ua, accept, "ru")
					if resp.StatusCode != http.StatusOK || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") || body != before {
						t.Errorf("приложению изменили подписку: UA=%q Accept=%q тип=%q", ua, accept, resp.Header.Get("Content-Type"))
					}
				}
			}
		})
	}
	// Наш клиент выполняет настоящий запрос своим транспортом: ?format=json,
	// без Accept, со стандартным Go-http-client/1.1. Windows и Android общие.
	sub, err := client.FetchSubscription(context.Background(), h.server.URL+path, nil)
	if err != nil || len(sub.Nodes) != 1 || sub.TrafficLimit != 4096 {
		t.Fatalf("наш клиент не прочитал подписку: %+v, %v", sub, err)
	}
}

func TestExplicitFormatsAndNonNavigationRequestsKeepSubscriptions(t *testing.T) {
	h := newHarness(t)
	h.createNode("nl")
	created := h.createUser(0, panel.CredVLESS)
	path := "/sub/" + created.User.SubToken
	for _, format := range []string{"json", "raw", "base64", "unknown", ""} {
		resp, body := buyerRequest(t, h, path+"?format="+format, browserAgent, "text/html", "ru")
		if strings.Contains(resp.Header.Get("Content-Type"), "html") {
			t.Fatalf("явный формат %q заменён страницей", format)
		}
		switch format {
		case "json":
			if !json.Valid([]byte(body)) {
				t.Error("наш формат не JSON")
			}
		case "raw":
			if !strings.HasPrefix(body, "vless://") {
				t.Error("сырой формат перестал содержать ссылки")
			}
		default:
			if _, err := base64.StdEncoding.DecodeString(body); err != nil {
				t.Errorf("формат %q не остался base64", format)
			}
		}
	}
	for _, accept := range []string{"", "*/*", "text/html;q=0, */*;q=1", "text/html;q=NaN", "text/html;q=2", "application/xhtml+xml"} {
		resp, _ := buyerRequest(t, h, path, browserAgent, accept, "ru")
		if strings.Contains(resp.Header.Get("Content-Type"), "html") {
			t.Errorf("страница без разрешённого HTML: Accept=%q", accept)
		}
	}
	for _, metadata := range []map[string]string{{"Sec-Fetch-Mode": "cors"}, {"Sec-Fetch-Dest": "empty"}} {
		req, _ := http.NewRequest(http.MethodGet, h.server.URL+path, nil)
		req.Header.Set("User-Agent", browserAgent)
		req.Header.Set("Accept", "text/html")
		for key, value := range metadata {
			req.Header.Set(key, value)
		}
		resp, err := h.server.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if strings.Contains(resp.Header.Get("Content-Type"), "html") {
			t.Errorf("запрос приложения с браузерным UA получил страницу: %v", metadata)
		}
	}
}

func walkBuyerPage(t *testing.T, page string, visit func(*html.Node)) {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			visit(node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
}

// Страница не грузит ничего с чужих доменов; внешняя поддержка — только переход.
func TestBuyerPageLoadsNothingFromOtherDomains(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0)
	h.do(http.MethodPut, "/api/v1/seller", adminToken, map[string]any{
		"support_url": "https://support.example/contact", "renew_url": "https://pay.example/renew",
		"announce": `<link rel="stylesheet" href="https://evil.example/style"><img src="https://evil.example/pixel">`,
	}, nil)
	resp, page := buyerRequest(t, h, "/sub/"+created.User.SubToken, browserAgent, "text/html", "ru")
	walkBuyerPage(t, page, func(node *html.Node) {
		switch node.Data {
		case "script", "iframe", "object", "embed", "base", "form":
			t.Errorf("страница может загрузить или отправить чужой ресурс: %s", node.Data)
		}
		for _, attr := range node.Attr {
			if attr.Key == "src" || attr.Key == "srcset" || attr.Key == "poster" || (attr.Key == "href" && node.Data == "link") {
				if !strings.HasPrefix(attr.Val, "data:") {
					t.Errorf("сетевой ресурс %s %s=%q", node.Data, attr.Key, attr.Val)
				}
			}
		}
		if node.Data == "style" && node.FirstChild != nil {
			css := strings.ToLower(node.FirstChild.Data)
			if strings.Contains(css, "url(") || strings.Contains(css, "@import") || strings.Contains(css, "@font-face") {
				t.Error("стили могут загрузить сторонний ресурс")
			}
		}
	})
	policy := resp.Header.Get("Content-Security-Policy")
	for _, directive := range []string{"default-src 'none'", "script-src 'none'", "style-src 'nonce-", "img-src data:", "frame-ancestors 'none'", "form-action 'none'", "base-uri 'none'"} {
		if !strings.Contains(policy, directive) {
			t.Errorf("нет ограничения CSP %q", directive)
		}
	}
	if strings.Contains(policy, "unsafe-inline") || strings.Contains(policy, "https:") || strings.Contains(policy, "'self'") {
		t.Error("политика разрешила сетевые ресурсы или произвольные стили")
	}
}

func TestQRCodeImportsTheExactSubscriptionEvenWithABrowserUserAgent(t *testing.T) {
	h := newHarness(t)
	h.createNode("nl")
	created := h.createUser(0, panel.CredVLESS)
	_, page := buyerRequest(t, h, "/sub/"+created.User.SubToken, browserAgent, "text/html", "ru")
	var imageURL string
	walkBuyerPage(t, page, func(node *html.Node) {
		if node.Data == "img" {
			for _, attr := range node.Attr {
				if attr.Key == "src" {
					imageURL = attr.Val
				}
			}
		}
	})
	pngBytes, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(imageURL, "data:image/png;base64,"))
	if err != nil || imageURL == "" {
		t.Fatalf("QR-код не является встроенным PNG: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatal(err)
	}
	bitmap, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		t.Fatal(err)
	}
	result, err := qrcode.NewQRCodeReader().Decode(bitmap, nil)
	if err != nil {
		t.Fatalf("независимый сканер не прочитал QR-код: %v", err)
	}
	want := created.Links.Subscription + "?format=base64"
	if result.GetText() != want || !strings.Contains(page, want) {
		t.Fatalf("QR ведёт не на показанную подписку: %q", result.GetText())
	}
	resp, body := buyerRequest(t, h, "/sub/"+created.User.SubToken+"?format=base64", browserAgent, "text/html", "ru")
	if _, err := base64.StdEncoding.DecodeString(body); err != nil || strings.Contains(resp.Header.Get("Content-Type"), "html") {
		t.Fatal("приложение с подменённым UA получило страницу по QR")
	}
}

func TestBuyerPageUsesTheBrowserLanguageAndKeepsFreshNonces(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0)
	nonces := map[string]bool{}
	for _, tc := range []struct{ language, lang, title string }{
		{"ru-RU,ru;q=0.9,en;q=0.8", "ru", "Ваш доступ"},
		{"en-GB,en;q=0.9,ru;q=0.5", "en", "Your access"},
		{"ru;q=0.2,en;q=0.9", "en", "Your access"},
		{"en;q=0,ru;q=1", "ru", "Ваш доступ"},
		{"de", "ru", "Ваш доступ"}, {"", "ru", "Ваш доступ"},
	} {
		resp, page := buyerRequest(t, h, "/sub/"+created.User.SubToken, browserAgent, "text/html", tc.language)
		if resp.Header.Get("Content-Language") != tc.lang || !strings.Contains(page, `<html lang="`+tc.lang+`">`) || !strings.Contains(page, tc.title) {
			t.Errorf("язык %q не выбран: %q", tc.language, resp.Header.Get("Content-Language"))
		}
		var nonce string
		walkBuyerPage(t, page, func(node *html.Node) {
			if node.Data == "style" {
				for _, attr := range node.Attr {
					if attr.Key == "nonce" {
						nonce = attr.Val
					}
				}
			}
		})
		if nonce == "" || nonces[nonce] || !strings.Contains(resp.Header.Get("Content-Security-Policy"), "'nonce-"+nonce+"'") {
			t.Fatal("nonce отсутствует, повторился или не совпал с CSP")
		}
		nonces[nonce] = true
	}
}

func TestBuyerCanDownloadOnlyCompleteAppsAndFollowConfiguredSellerLinks(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0)
	path := "/sub/" + created.User.SubToken
	_, page := buyerRequest(t, h, path, browserAgent, "text/html", "ru")
	if strings.Contains(page, "/app/") || strings.Contains(page, ">Продлить</a>") || strings.Contains(page, ">Поддержка</a>") {
		t.Fatal("страница обещает отсутствующие действия")
	}
	for name, body := range map[string]string{"marvia-windows.exe": "неполный файл", "marvia-android.apk": fakeAPK} {
		if err := os.WriteFile(filepath.Join(h.dist, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	h.do(http.MethodPut, "/api/v1/seller", adminToken, map[string]any{
		"support_url": "tg://resolve?domain=seller_support", "renew_url": "https://pay.example/renew", "announce": "Сегодня всё работает",
	}, nil)
	_, page = buyerRequest(t, h, path, browserAgent, "text/html", "ru")
	if strings.Contains(page, "/app/windows") || !strings.Contains(page, `href="`+path+`/app/android"`) || !strings.Contains(page, `href="tg://resolve?domain=seller_support"`) || !strings.Contains(page, `href="https://pay.example/renew"`) || !strings.Contains(page, "Сегодня всё работает") || strings.Contains(page, "ZgotmplZ") {
		t.Fatal("страница не выполнила обещания загрузки и связи с продавцом")
	}
	resp, body := buyerRequest(t, h, path+"/app/android", browserAgent, "", "")
	if resp.StatusCode != http.StatusOK || body != fakeAPK {
		t.Fatal("кнопка скачивания не отдаёт приложение")
	}
	if err := os.WriteFile(filepath.Join(h.dist, "marvia-windows.zip"), []byte("комплект"), 0o600); err != nil {
		t.Fatal(err)
	}
	androidAgent := "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/135.0.0.0 Mobile Safari/537.36"
	_, page = buyerRequest(t, h, path, androidAgent, "text/html", "en")
	if strings.Index(page, "/app/android") >= strings.Index(page, "/app/windows") || !strings.Contains(page, "Download for Android") {
		t.Fatal("Android не получил свою загрузку первой")
	}
	_, page = buyerRequest(t, h, path, browserAgent, "text/html", "ru")
	if strings.Index(page, "/app/windows") >= strings.Index(page, "/app/android") {
		t.Fatal("Windows не получил свою загрузку первой")
	}
	resp, body = buyerRequest(t, h, path+"/app/windows", browserAgent, "", "")
	if resp.StatusCode != http.StatusOK || body != "комплект" {
		t.Fatal("кнопка Windows не отдаёт полный комплект")
	}
}

func TestExpiredAndExhaustedBuyersStillSeeHowToRenew(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update map[string]any
		want   string
	}{
		{"без ограничений", map[string]any{}, "Без ограничения по сроку"},
		{"будущий срок", map[string]any{"expires_at": "2035-10-05T15:04:00Z"}, "Действует до 05.10.2035, 15:04 UTC"},
		{"истёкший срок", map[string]any{"expires_at": "2020-10-05T15:04:00Z"}, "Срок закончился 05.10.2020, 15:04 UTC"},
		{"отключённый доступ", map[string]any{"enabled": false}, "Доступ отключён продавцом"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			created := h.createUser(1024)
			h.do(http.MethodPatch, fmt.Sprintf("/api/v1/users/%d", created.User.ID), adminToken, tc.update, nil)
			h.do(http.MethodPut, "/api/v1/seller", adminToken, map[string]any{"renew_url": "https://pay.example/renew"}, nil)
			resp, page := buyerRequest(t, h, "/sub/"+created.User.SubToken, browserAgent, "text/html", "ru")
			if resp.StatusCode != http.StatusOK || !strings.Contains(page, tc.want) || !strings.Contains(page, "Осталось 1 КБ из 1 КБ") || !strings.Contains(page, ">Продлить</a>") {
				t.Fatalf("покупатель не увидел состояние и продление: %s", page)
			}
		})
	}
	store, userID, nodeID := usageStore(t)
	ctx := context.Background()
	limit := int64(1024)
	user, err := store.UpdateUser(ctx, userID, panel.UpdateUserParams{TrafficLimit: &limit})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSellerInfo(ctx, seller.Info{RenewURL: "https://pay.example/renew"}); err != nil {
		t.Fatal(err)
	}
	api := panel.NewAPI(store, adminToken, "https://seller.example", t.TempDir())
	for _, used := range []int64{256, 2048} {
		report(t, store, nodeID, userID, 0, used)
		req := httptest.NewRequest(http.MethodGet, "/sub/"+user.SubToken, nil)
		req.Header.Set("User-Agent", browserAgent)
		req.Header.Set("Accept", "text/html")
		w := httptest.NewRecorder()
		api.Handler().ServeHTTP(w, req)
		want := "Осталось 768 Б из 1 КБ"
		if used > 1024 {
			want = "Осталось 0 Б из 1 КБ"
			if !strings.Contains(w.Body.String(), "Трафик закончился") {
				t.Error("исчерпание квоты не объяснено")
			}
		}
		if !strings.Contains(w.Body.String(), want) || !strings.Contains(w.Body.String(), ">Продлить</a>") {
			t.Error("не показан остаток и путь продления")
		}
	}
}

func TestOpeningBuyerPageLeavesNoCookiesOrVisitorLogs(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(0)
	before := events(t, h)
	var serverLog bytes.Buffer
	old := log.Writer()
	log.SetOutput(&serverLog)
	defer log.SetOutput(old)
	resp, _ := buyerRequest(t, h, "/sub/"+created.User.SubToken, browserAgent, "text/html", "ru")
	if len(resp.Cookies()) != 0 || resp.Header.Get("Cache-Control") != "no-store" || resp.Header.Get("Referrer-Policy") != "no-referrer" || resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Error("страница сохраняет cookie, кэш или разрешает утечку адреса")
	}
	if len(events(t, h)) != len(before) || serverLog.Len() != 0 {
		t.Error("просмотр попал в журнал")
	}
	for _, header := range []string{"Accept", "User-Agent", "Accept-Language"} {
		if !strings.Contains(resp.Header.Get("Vary"), header) {
			t.Errorf("ответ не учитывает заголовок %s", header)
		}
	}
	for _, token := range []string{"чужой", created.User.SubToken} {
		if token == created.User.SubToken {
			h.do(http.MethodPost, fmt.Sprintf("/api/v1/users/%d/sub-token", created.User.ID), adminToken, nil, nil)
		}
		resp, _ := buyerRequest(t, h, "/sub/"+token, browserAgent, "text/html", "ru")
		if resp.StatusCode != http.StatusNotFound || strings.Contains(resp.Header.Get("Content-Type"), "html") {
			t.Error("страница доступна по чужому или сменённому токену")
		}
	}
}

func TestRequestHeadersCannotReplaceTheSubscriptionURLOrSellerLink(t *testing.T) {
	store, userID, _ := usageStore(t)
	user, err := store.GetUser(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSellerInfo(context.Background(), seller.Info{
		SupportURL: "javascript:alert(1)", RenewURL: "https://pay.example/renew", Announce: "<script>alert(2)</script>",
	}); err != nil {
		t.Fatal(err)
	}
	api := panel.NewAPI(store, adminToken, "https://seller.example", t.TempDir())
	req := httptest.NewRequest(http.MethodGet, "https://attacker.example/sub/"+user.SubToken, nil)
	req.Header.Set("User-Agent", browserAgent)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Forwarded", "host=attacker.example;proto=http")
	req.Header.Set("X-Forwarded-Host", "attacker.example")
	req.Header.Set("X-Forwarded-Proto", "http")
	req.RemoteAddr = "192.0.2.1:12345"
	w := httptest.NewRecorder()
	api.Handler().ServeHTTP(w, req)
	page := w.Body.String()
	if w.Code != http.StatusOK || strings.Contains(page, "attacker.example") || !strings.Contains(page, "https://seller.example/sub/"+user.SubToken+"?format=base64") || strings.Contains(page, "javascript:") || strings.Contains(page, ">Поддержка</a>") || strings.Contains(page, "<script>") {
		t.Fatal("чужие заголовки или испорченная запись продавца подменили страницу")
	}
}
