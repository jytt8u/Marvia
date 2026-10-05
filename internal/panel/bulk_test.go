package panel_test

import (
	"encoding/csv"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
)

type bulkResponse struct {
	Done   int     `json:"done"`
	Failed []int64 `json:"failed"`
}

// После аварии продавец продлевает всех пострадавших разом, а не по одному.
func TestBulkRenewByPlanRenewsEveryChosenClientOnce(t *testing.T) {
	h := newHarness(t)
	plan := month(h)
	a, b, c := h.createUser(0), h.createUser(0), h.createUser(0)

	var out bulkResponse
	if code := h.withKey(http.MethodPost, "/api/v1/users/bulk", "авария-5-окт",
		map[string]any{"ids": []int64{a.User.ID, b.User.ID}, "action": "renew", "plan_id": plan.ID}, &out); code != http.StatusOK {
		t.Fatalf("массовое продление: код %d", code)
	}
	// Повтор того же запроса — например, двойной клик — второй месяц не добавляет.
	h.withKey(http.MethodPost, "/api/v1/users/bulk", "авария-5-окт",
		map[string]any{"ids": []int64{a.User.ID, b.User.ID}, "action": "renew", "plan_id": plan.ID}, nil)

	if out.Done != 2 || len(out.Failed) != 0 {
		t.Fatalf("продлено %d, не вышло %v", out.Done, out.Failed)
	}
	for _, id := range []int64{a.User.ID, b.User.ID} {
		u := h.getUser(id)
		if u.ExpiresAt == nil || absDur(time.Until(*u.ExpiresAt)-30*24*time.Hour) > time.Minute {
			t.Fatalf("покупатель %d: срок %v, ожидалось +30 дней один раз", id, u.ExpiresAt)
		}
	}
	if h.getUser(c.User.ID).ExpiresAt != nil {
		t.Fatal("невыбранного покупателя тоже продлили")
	}
}

func TestBulkDisableAndEnableTouchOnlyChosen(t *testing.T) {
	h := newHarness(t)
	a, b := h.createUser(0), h.createUser(0)
	h.do(http.MethodPost, "/api/v1/users/bulk", adminToken, map[string]any{"ids": []int64{a.User.ID}, "action": "disable"}, nil)
	if h.getUser(a.User.ID).Enabled || !h.getUser(b.User.ID).Enabled {
		t.Fatal("отключён не тот, кого выбрали")
	}
	h.do(http.MethodPost, "/api/v1/users/bulk", adminToken, map[string]any{"ids": []int64{a.User.ID}, "action": "enable"}, nil)
	if !h.getUser(a.User.ID).Enabled {
		t.Fatal("покупатель не включился обратно")
	}
}

// Несуществующий номер в списке не срывает остальных, а возвращается в
// failed: продавец видит, кого не удалось, а не «ошибка» на всю пачку.
func TestBulkReportsMissingClientsInsteadOfFailingAll(t *testing.T) {
	h := newHarness(t)
	a := h.createUser(0)
	var out bulkResponse
	h.do(http.MethodPost, "/api/v1/users/bulk", adminToken,
		map[string]any{"ids": []int64{a.User.ID, 99999}, "action": "extend", "extend_by": "7d"}, &out)
	if out.Done != 1 || len(out.Failed) != 1 || out.Failed[0] != 99999 {
		t.Fatalf("итог: %+v", out)
	}
}

func TestBulkRefusesUnknownActionAndHugeLists(t *testing.T) {
	h := newHarness(t)
	if code := h.do(http.MethodPost, "/api/v1/users/bulk", adminToken, map[string]any{"ids": []int64{1}, "action": "explode"}, nil); code != http.StatusBadRequest {
		t.Fatalf("неизвестное действие: код %d", code)
	}
	ids := make([]int64, 5001)
	if code := h.do(http.MethodPost, "/api/v1/users/bulk", adminToken, map[string]any{"ids": ids, "action": "disable"}, nil); code != http.StatusBadRequest {
		t.Fatalf("пять тысяч номеров за раз: код %d", code)
	}
}

// Выгрузка нужна для учёта и переезда, а не для того, чтобы унести доступы:
// в ней нет ни токенов подписки, ни секретов.
func TestClientsCSVHasNoSecrets(t *testing.T) {
	h := newHarness(t)
	created := h.createUser(1000, "vp1", "vless", "trojan")
	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/users.csv", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(resp.Header.Get("Content-Type"), "csv") {
		t.Fatalf("выгрузка: код %d, тип %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	body := string(raw)
	for _, secret := range []string{created.User.SubToken, created.secretOf(panel.CredVLESS), created.secretOf(panel.CredTrojan)} {
		if secret != "" && strings.Contains(body, secret) {
			t.Fatal("в выгрузку попал секрет покупателя")
		}
	}
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(body, string([]byte{0xEF, 0xBB, 0xBF})))).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][1] != "заказ 1043" {
		t.Fatalf("выгрузка не читается как CSV: %v, строк %d", err, len(rows))
	}
}

// Метку пишет продавец, а открывают выгрузку в Excel: строка, начинающаяся с
// «=», стала бы там формулой.
func TestClientsCSVDoesNotLetLabelBecomeFormula(t *testing.T) {
	h := newHarness(t)
	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "=HYPERLINK(\"http://x\")"}, nil)
	req, _ := http.NewRequest(http.MethodGet, h.server.URL+"/api/v1/users.csv", nil)
	req.Header.Set("Authorization", "Bearer "+adminToken)
	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	rows, _ := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(raw), string([]byte{0xEF, 0xBB, 0xBF})))).ReadAll()
	if strings.HasPrefix(rows[1][1], "=") {
		t.Fatalf("метка ушла в CSV формулой: %q", rows[1][1])
	}
}
