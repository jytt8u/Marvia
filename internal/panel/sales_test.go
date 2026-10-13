package panel_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
)

type statsSales struct {
	Sales panel.SalesReport `json:"sales"`
}

func (h *harness) sales() panel.SalesReport {
	h.t.Helper()
	var out statsSales
	if code := h.do(http.MethodGet, "/api/v1/stats?days=30", adminToken, nil, &out); code != http.StatusOK {
		h.t.Fatalf("статистика: код %d", code)
	}
	return out.Sales
}

func pricedMonth(h *harness) panel.Plan {
	return h.createPlan(map[string]any{"name": "Месяц", "days": 30, "price": 300, "currency": "₽"})
}

func TestSaleAndRenewalByPlanAddUpToRevenue(t *testing.T) {
	h := newHarness(t)
	plan := pricedMonth(h)

	var out createUserResponse
	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "Анна", "plan_id": plan.ID}, &out)
	h.do(http.MethodPost, "/api/v1/users/"+itoa(out.User.ID)+"/renew", adminToken, map[string]any{"plan_id": plan.ID}, nil)

	got := h.sales()
	if len(got.Money) != 1 {
		t.Fatalf("выручка: %+v", got.Money)
	}
	m := got.Money[0]
	if m.Currency != "₽" || m.Sum != 600 || m.Sales != 2 || m.New != 1 || m.Renew != 1 {
		t.Fatalf("ожидалось 600 ₽ за одну продажу и одно продление, вышло %+v", m)
	}
	if len(got.ByPlan) != 1 || got.ByPlan[0].Name != "Месяц" || got.ByPlan[0].Count != 2 {
		t.Fatalf("по тарифам: %+v", got.ByPlan)
	}
}

// Платёжная система повторяет уведомление, и бот присылает то же продление
// второй раз. Срок он не удваивает — и выручку тоже не должен.
func TestRepeatedPaymentDoesNotCountRevenueTwice(t *testing.T) {
	h := newHarness(t)
	plan := pricedMonth(h)
	u := h.createUser(0)

	for range 2 {
		h.withKey(http.MethodPost, "/api/v1/users/"+itoa(u.User.ID)+"/renew", "оплата-7781", map[string]any{"plan_id": plan.ID}, nil)
	}
	if m := h.sales().Money; len(m) != 1 || m[0].Sum != 300 || m[0].Sales != 1 {
		t.Fatalf("повтор оплаты посчитан дважды: %+v", m)
	}
}

// Продление всех пострадавших после аварии — подарок, а не сотня оплат.
func TestBulkRenewalIsNotCountedAsRevenue(t *testing.T) {
	h := newHarness(t)
	plan := pricedMonth(h)
	a, b := h.createUser(0), h.createUser(0)
	h.do(http.MethodPost, "/api/v1/users/bulk", adminToken,
		map[string]any{"ids": []int64{a.User.ID, b.User.ID}, "action": "renew", "plan_id": plan.ID}, nil)

	if m := h.sales().Money; len(m) != 0 {
		t.Fatalf("массовое продление попало в выручку: %+v", m)
	}
}

// Покупатель со своими сроком и лимитами заведён не по тарифу, и цены у
// него нет — придумывать её панель не должна.
func TestClientWithOwnLimitsIsNotASale(t *testing.T) {
	h := newHarness(t)
	pricedMonth(h)
	h.createUser(0)
	if m := h.sales().Money; len(m) != 0 {
		t.Fatalf("продажа без тарифа: %+v", m)
	}
}

// Удалённый покупатель остаётся цифрой в выручке, но книга не хранит, кто
// он: связать продажу с человеком после удаления нечем.
func TestSalesSurviveDeletionWithoutKnowingTheBuyer(t *testing.T) {
	h := newHarness(t)
	plan := pricedMonth(h)
	var out createUserResponse
	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "Анна", "plan_id": plan.ID}, &out)
	h.do(http.MethodDelete, "/api/v1/users/"+itoa(out.User.ID), adminToken, nil, nil)

	if m := h.sales().Money; len(m) != 1 || m[0].Sum != 300 {
		t.Fatalf("выручка пропала с покупателем: %+v", m)
	}
	for _, c := range h.store.Columns(context.Background(), "sales") {
		switch c {
		case "user_id", "label", "external_id", "at":
			t.Fatalf("в книге продаж есть столбец %q — по нему продажу можно связать с человеком", c)
		}
	}
}

// Переименованный или подорожавший тариф не переписывает прошлое: продан
// он был под тем именем и по той цене.
func TestPlanPriceChangeDoesNotRewriteSoldOnes(t *testing.T) {
	h := newHarness(t)
	plan := pricedMonth(h)
	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "Анна", "plan_id": plan.ID}, nil)
	h.do(http.MethodPatch, "/api/v1/plans/"+itoa(plan.ID), adminToken,
		map[string]any{"name": "Месяц+", "days": 30, "price": 500, "currency": "₽"}, nil)

	got := h.sales()
	if got.Money[0].Sum != 300 || got.ByPlan[0].Name != "Месяц" {
		t.Fatalf("правка тарифа переписала проданное: %+v %+v", got.Money, got.ByPlan)
	}
}

func TestPlanRefusesNegativeOrAbsurdPrice(t *testing.T) {
	h := newHarness(t)
	for _, price := range []int64{-1, 2_000_000_000} {
		if code := h.do(http.MethodPost, "/api/v1/plans", adminToken,
			map[string]any{"name": "Месяц", "days": 30, "price": price}, nil); code != http.StatusBadRequest {
			t.Fatalf("цена %d: код %d", price, code)
		}
	}
}
