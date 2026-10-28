package panel_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
)

// floodPanel — панель с боевыми пределами и часами, которые двигает тест.
type floodPanel struct {
	t       *testing.T
	handler http.Handler
	at      time.Time
	sub     string // токен подписки настоящего покупателя
}

func newFloodPanel(t *testing.T) *floodPanel {
	t.Helper()
	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	user, _, err := store.CreateUser(context.Background(), panel.CreateUserParams{Label: "покупатель"})
	if err != nil {
		t.Fatal(err)
	}
	p := &floodPanel{t: t, at: time.Unix(1_700_000_000, 0), sub: user.SubToken}
	api := panel.NewAPI(store, adminToken, "https://sub.example.com", t.TempDir())
	api.GuardAgainstFlood(func() time.Time { return p.at })
	p.handler = api.Handler()
	return p
}

// get — запрос с адреса from. Адрес ставится прямо в RemoteAddr: так панель
// видит покупателя, пришедшего без прокси.
func (p *floodPanel) get(from, path, token string) *httptest.ResponseRecorder {
	p.t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = from + ":40000"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	p.handler.ServeHTTP(rec, req)
	return rec
}

// TestSubscriptionTokenGuessingGets429WhileOthersAreServed — адрес, который
// перебирает токены подписки, после сотни промахов получает 429 с
// Retry-After на всё подряд, а покупатели с других адресов в это время
// получают подписки как обычно. Через десять минут блокировка спадает.
func TestSubscriptionTokenGuessingGets429WhileOthersAreServed(t *testing.T) {
	p := newFloodPanel(t)
	const guesser, buyer = "203.0.113.66", "198.51.100.20"

	for i := range panel.FloodFailures() {
		if code := p.get(guesser, fmt.Sprintf("/sub/угадываю-%d", i), "").Code; code != http.StatusNotFound {
			t.Fatalf("промах %d: ответ %d, ждали 404 — блокировка не должна наступать раньше срока", i+1, code)
		}
	}

	blocked := p.get(guesser, "/sub/"+p.sub, "")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("после %d промахов ответ %d, ждали 429", panel.FloodFailures(), blocked.Code)
	}
	wait, err := strconv.Atoi(blocked.Header().Get("Retry-After"))
	if err != nil || wait < 1 || wait > 600 {
		t.Fatalf("Retry-After %q: ждали секунды до конца блокировки", blocked.Header().Get("Retry-After"))
	}

	if code := p.get(buyer, "/sub/"+p.sub, "").Code; code != http.StatusOK {
		t.Fatalf("покупатель с другого адреса получил %d во время перебора", code)
	}
	if code := p.get("198.51.100.21", "/sub/ошибся", "").Code; code != http.StatusNotFound {
		t.Fatalf("одна ошибка с чистого адреса дала %d, ждали 404", code)
	}

	p.at = p.at.Add(10*time.Minute + time.Second)
	if code := p.get(guesser, "/sub/"+p.sub, "").Code; code != http.StatusOK {
		t.Fatalf("через десять минут адрес всё ещё заблокирован: %d", code)
	}
}

// TestRequestFloodFromOneAddressIsCutWhileAnotherIsServed — поток даже
// правильных запросов с одного адреса упирается в запас и получает 429, а
// соседний адрес обслуживается.
func TestRequestFloodFromOneAddressIsCutWhileAnotherIsServed(t *testing.T) {
	p := newFloodPanel(t)

	var served, cut int
	for range 500 {
		switch rec := p.get("203.0.113.7", "/healthz", ""); rec.Code {
		case http.StatusOK:
			served++
		case http.StatusTooManyRequests:
			cut++
			if rec.Header().Get("Retry-After") == "" {
				t.Fatal("429 без Retry-After: клиенту не сказано, когда повторить")
			}
		default:
			t.Fatalf("неожиданный ответ %d", rec.Code)
		}
	}
	if served > 150 || cut == 0 {
		t.Fatalf("из 500 запросов разом пропущено %d, отклонено %d: флуд не отсечён", served, cut)
	}
	if code := p.get("198.51.100.20", "/sub/"+p.sub, "").Code; code != http.StatusOK {
		t.Fatalf("соседний адрес получил %d во время флуда", code)
	}
}

// TestSellerPageNodesAndCGNATAreNotCutAtNormalLoad — обычная нагрузка в
// пределы не упирается: страница панели, загружающаяся разом и потом
// спрашивающая раз в полторы секунды; нода с отозванным токеном, которая
// обязана слышать 401, а не 429 — по нему она узнаёт отзыв; сотни
// покупателей за одним адресом мобильного оператора, среди них удалённые.
func TestSellerPageNodesAndCGNATAreNotCutAtNormalLoad(t *testing.T) {
	p := newFloodPanel(t)
	const seller, node, cgnat = "198.51.100.50", "198.51.100.60", "198.51.100.70"

	expect := func(code, want int, who string) {
		t.Helper()
		if code != want {
			t.Fatalf("%s: ответ %d, ждали %d", who, code, want)
		}
	}

	// Загрузка страницы — пачка запросов разом.
	for range 40 {
		expect(p.get(seller, "/api/v1/users", adminToken).Code, http.StatusOK, "загрузка страницы")
	}
	// Десять минут работы: страница каждые 1,5 с, нода дважды за 15 с,
	// за адресом оператора — подписка раз в секунду и удалённый покупатель
	// раз в двадцать секунд.
	start := p.at
	for step := 0; p.at.Sub(start) < 10*time.Minute; step++ {
		p.at = p.at.Add(500 * time.Millisecond)
		if step%3 == 0 {
			expect(p.get(seller, "/api/v1/users", adminToken).Code, http.StatusOK, "опрос страницы")
		}
		if step%15 == 0 {
			expect(p.get(node, "/api/v1/node/users", "отозванный-токен-ноды").Code, http.StatusUnauthorized, "нода с отозванным токеном")
		}
		if step%2 == 0 {
			expect(p.get(cgnat, "/sub/"+p.sub, "").Code, http.StatusOK, "покупатель за CGNAT")
		}
		if step%40 == 0 {
			expect(p.get(cgnat, "/sub/удалённый-покупатель", "").Code, http.StatusNotFound, "удалённый покупатель за CGNAT")
		}
	}
}
