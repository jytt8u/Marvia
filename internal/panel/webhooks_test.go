package panel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testWebhookSecret = "0123456789abcdef0123456789abcdef-secret"

// webhookBot — бот продавца: принимает вебхуки и помнит, что пришло. Ответ
// можно заставить падать, чтобы проверить повторы.
type webhookBot struct {
	mu       sync.Mutex
	got      []*http.Request
	bodies   [][]byte
	failNext int
}

func (b *webhookBot) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	b.mu.Lock()
	defer b.mu.Unlock()
	b.got = append(b.got, r)
	b.bodies = append(b.bodies, body)
	if b.failNext > 0 {
		b.failNext--
		http.Error(w, "бот лежит", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (b *webhookBot) count() int { b.mu.Lock(); defer b.mu.Unlock(); return len(b.got) }

// openWebhookStore — панель с включёнными вебхуками на адрес бота.
//
// Сам бот слушает 127.0.0.1, а настоящая доставка туда не пустит — это и
// есть защита от SSRF. Поэтому в настройки пишем внешнее имя, а транспорт в
// тесте ведёт любое соединение на тестовый сервер: проверка адреса и подпись
// остаются настоящими.
func openWebhookStore(t *testing.T, path string, bot *webhookBot) (*Store, *Webhooks) {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	srv := httptest.NewTLSServer(bot)
	t.Cleanup(srv.Close)
	if err := s.SetWebhookSettings(context.Background(), WebhookSettings{
		Enabled: true, URLs: []string{"https://bot.example.com/marvia"}, Secret: testWebhookSecret,
	}); err != nil {
		t.Fatal(err)
	}
	d := NewWebhooks(s)
	d.client = &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
			},
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // тестовый сервер
		},
		CheckRedirect: d.client.CheckRedirect,
	}
	return s, d
}

func TestNewClientReachesTheBotSignedAndWithoutSecrets(t *testing.T) {
	bot := &webhookBot{}
	s, d := openWebhookStore(t, filepath.Join(t.TempDir(), "panel.db"), bot)
	user, issued, err := s.CreateUser(context.Background(), CreateUserParams{Label: "Анна", Kinds: []string{CredVP1, CredVLESS}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Deliver(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if bot.count() != 1 {
		t.Fatalf("бот получил %d вебхуков, ожидался 1", bot.count())
	}
	req, body := bot.got[0], string(bot.bodies[0])
	if req.Header.Get("X-Marvia-Event") != EventUserCreate || !strings.Contains(body, `"label":"Анна"`) {
		t.Fatalf("событие %q, тело %s", req.Header.Get("X-Marvia-Event"), body)
	}
	mac := hmac.New(sha256.New, []byte(testWebhookSecret))
	mac.Write(bot.bodies[0])
	if req.Header.Get("X-Marvia-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("подпись не сходится с секретом")
	}
	for _, secret := range []string{user.SubToken, issued[0].Secret, issued[1].Secret} {
		if strings.Contains(body, secret) {
			t.Fatal("в вебхук попал секрет покупателя")
		}
	}
}

// Бот упал — панель повторяет с паузой, и повтор несёт тот же
// X-Marvia-Delivery: по нему бот узнаёт уже обработанное.
func TestRetryCarriesTheSameDeliveryID(t *testing.T) {
	bot := &webhookBot{failNext: 1}
	s, d := openWebhookStore(t, filepath.Join(t.TempDir(), "panel.db"), bot)
	if _, _, err := s.CreateUser(context.Background(), CreateUserParams{Label: "Борис"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	_ = d.Deliver(context.Background(), now)
	if err := d.Deliver(context.Background(), now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if bot.count() != 1 {
		t.Fatalf("повтор пришёл раньше паузы: вебхуков %d", bot.count())
	}
	if err := d.Deliver(context.Background(), now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if bot.count() != 2 {
		t.Fatalf("после паузы повтора нет: вебхуков %d", bot.count())
	}
	first, second := bot.got[0].Header.Get("X-Marvia-Delivery"), bot.got[1].Header.Get("X-Marvia-Delivery")
	if first == "" || first != second || string(bot.bodies[0]) != string(bot.bodies[1]) {
		t.Fatalf("повтор не узнаваем: %q и %q", first, second)
	}
}

func TestUndeliveredWebhookSurvivesPanelRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "panel.db")
	down := &webhookBot{failNext: 100}
	s, d := openWebhookStore(t, path, down)
	if _, _, err := s.CreateUser(context.Background(), CreateUserParams{Label: "Вера"}); err != nil {
		t.Fatal(err)
	}
	_ = d.Deliver(context.Background(), time.Now().UTC())
	_ = s.Close()

	up := &webhookBot{}
	_, d2 := openWebhookStore(t, path, up)
	if err := d2.Deliver(context.Background(), time.Now().UTC().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if up.count() != 1 || up.got[0].Header.Get("X-Marvia-Event") != EventUserCreate {
		t.Fatalf("после перезапуска доставлено %d", up.count())
	}
}

// Истечение — переход, а не состояние: бот получает его один раз, а не
// каждую минуту, пока покупатель не продлил.
func TestExpiryIsReportedOnceNotEveryMinute(t *testing.T) {
	bot := &webhookBot{}
	s, d := openWebhookStore(t, filepath.Join(t.TempDir(), "panel.db"), bot)
	past := time.Now().UTC().Add(-time.Hour)
	if _, _, err := s.CreateUser(context.Background(), CreateUserParams{Label: "Глеб", ExpiresAt: &Expiry{past}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for i := 0; i < 3; i++ {
		if err := d.Observe(context.Background(), now.Add(time.Duration(i)*time.Minute)); err != nil {
			t.Fatal(err)
		}
	}
	_ = d.Deliver(context.Background(), now.Add(5*time.Minute))
	expired := 0
	for _, r := range bot.got {
		if r.Header.Get("X-Marvia-Event") == WebhookUserExpired {
			expired++
		}
	}
	if expired != 1 {
		t.Fatalf("об истечении сообщено %d раз", expired)
	}
}

func TestWebhookCannotPointIntoThePanelsNetwork(t *testing.T) {
	for _, bad := range []string{
		"https://127.0.0.1/hook", "https://localhost/hook", "https://10.0.0.5/hook",
		"https://169.254.169.254/latest", "https://[::1]/hook", "http://bot.example.com/hook",
		"https://user:pass@bot.example.com/hook", "https://bot.example.com:25/hook",
	} {
		if _, err := checkWebhookURL(bad); err == nil {
			t.Errorf("адрес %s принят", bad)
		}
	}
	if _, err := checkWebhookURL("https://bot.example.com/marvia"); err != nil {
		t.Fatalf("обычный адрес бота отвергнут: %v", err)
	}
}

func TestWebhooksNeedALongSecret(t *testing.T) {
	_, err := WebhookSettings{Enabled: true, URLs: []string{"https://bot.example.com/h"}, Secret: "короткий"}.check()
	if err == nil {
		t.Fatal("короткий секрет принят")
	}
}

// Бот должен уметь проверить подпись сам — пример из docs/bot.md считает её
// так же, как панель.
func TestSignatureIsHMACOverTheExactBody(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"event": "user.create"})
	mac := hmac.New(sha256.New, []byte("секрет"))
	mac.Write(body)
	if webhookSignature("секрет", body) != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("подпись считается не так, как описано для бота")
	}
}
