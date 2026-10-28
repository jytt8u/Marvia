package panel_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/users"
)

const gb = int64(1) << 30

type planResponse struct {
	Plan panel.Plan `json:"plan"`
}

func (h *harness) createPlan(body map[string]any) panel.Plan {
	h.t.Helper()
	var out planResponse
	if code := h.do(http.MethodPost, "/api/v1/plans", adminToken, body, &out); code != http.StatusOK {
		h.t.Fatalf("создание тарифа: код %d", code)
	}
	return out.Plan
}

func (h *harness) getUser(id int64) panel.User {
	h.t.Helper()
	var out struct {
		User panel.User `json:"user"`
	}
	if code := h.do(http.MethodGet, "/api/v1/users/"+itoa(id), adminToken, nil, &out); code != http.StatusOK {
		h.t.Fatalf("покупатель %d: код %d", id, code)
	}
	return out.User
}

func month(h *harness) panel.Plan {
	return h.createPlan(map[string]any{
		"name": "Месяц", "days": 30, "traffic_limit": 50 * gb, "max_ips": 2, "note": "300 ₽",
	})
}

func TestClientFromPlanGetsItsTermAndLimits(t *testing.T) {
	h := newHarness(t)
	plan := month(h)

	var out createUserResponse
	if code := h.do(http.MethodPost, "/api/v1/users", adminToken,
		map[string]any{"label": "Анна", "plan_id": plan.ID}, &out); code != http.StatusOK {
		t.Fatalf("покупатель по тарифу: код %d", code)
	}
	u := out.User
	if u.TrafficLimit != 50*gb || u.MaxIPs != 2 {
		t.Fatalf("лимиты не из тарифа: трафик %d, устройств %d", u.TrafficLimit, u.MaxIPs)
	}
	if u.PlanID == nil || *u.PlanID != plan.ID {
		t.Fatalf("покупатель не помнит свой тариф: %v", u.PlanID)
	}
	if u.ExpiresAt == nil || absDur(time.Until(*u.ExpiresAt)-30*24*time.Hour) > time.Minute {
		t.Fatalf("срок не 30 дней: %v", u.ExpiresAt)
	}
}

// Тариф и свои лимиты в одном запросе — скорее всего ошибка в боте: молча
// выбрать одно из двух значило бы выдать не то, за что заплатили.
func TestPlanAndOwnLimitsInOneRequestAreRefused(t *testing.T) {
	h := newHarness(t)
	plan := month(h)
	code := h.do(http.MethodPost, "/api/v1/users", adminToken,
		map[string]any{"label": "Борис", "plan_id": plan.ID, "traffic_limit": 10 * gb}, nil)
	if code != http.StatusBadRequest {
		t.Fatalf("тариф вместе со своими лимитами: код %d, ожидался 400", code)
	}
}

func TestUnknownPlanIsRefusedNotIgnored(t *testing.T) {
	h := newHarness(t)
	code := h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "Вера", "plan_id": 999}, nil)
	if code != http.StatusBadRequest && code != http.StatusNotFound {
		t.Fatalf("несуществующий тариф: код %d", code)
	}
}

// Продление по тарифу — новый оплаченный период: срок добавляется к
// текущему окончанию, а расход начинается заново.
func TestRenewByPlanAddsTermToCurrentEndAndStartsTrafficAnew(t *testing.T) {
	h := newHarness(t)
	plan := month(h)
	node := h.createNode("fi-1")

	var created createUserResponse
	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "Глеб", "plan_id": plan.ID}, &created)
	before := *created.User.ExpiresAt

	account := h.nodeUsers(node.Token)[0].AccountID()
	h.do(http.MethodPost, "/api/v1/node/usage", node.Token,
		map[string]any{"usage": map[string]users.Usage{account: {Up: 10 * gb, Down: 30 * gb}}}, nil)
	if used := h.getUser(created.User.ID).Used; used != 40*gb {
		t.Fatalf("расход до продления %d", used)
	}

	if code := h.do(http.MethodPost, "/api/v1/users/"+itoa(created.User.ID)+"/renew", adminToken,
		map[string]any{"plan_id": plan.ID}, nil); code != http.StatusOK {
		t.Fatalf("продление: код %d", code)
	}
	after := h.getUser(created.User.ID)
	if absDur(after.ExpiresAt.Sub(before)-30*24*time.Hour) > time.Minute {
		t.Fatalf("срок продлён не от прежнего окончания: было %v, стало %v", before, after.ExpiresAt)
	}
	if after.Used != 0 {
		t.Fatalf("после продления расход %d, ожидался 0", after.Used)
	}
	// Нода видит полный тариф заново: её собственные 40 ГБ уже не в счёт.
	if got := h.nodeUsers(node.Token)[0]; !got.Enabled || got.TrafficLimit < 50*gb {
		t.Fatalf("нода после продления: включён %v, лимит %d", got.Enabled, got.TrafficLimit)
	}
}

func TestRepeatedRenewWithSameKeyAddsTermOnce(t *testing.T) {
	h := newHarness(t)
	plan := month(h)
	var created createUserResponse
	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "Дарья", "plan_id": plan.ID}, &created)
	path := "/api/v1/users/" + itoa(created.User.ID) + "/renew"

	for i := 0; i < 2; i++ {
		if code := h.withKey(http.MethodPost, path, "оплата-77", map[string]any{"plan_id": plan.ID}, nil); code != http.StatusOK {
			t.Fatalf("продление %d: код %d", i+1, code)
		}
	}
	got := h.getUser(created.User.ID)
	if absDur(time.Until(*got.ExpiresAt)-60*24*time.Hour) > time.Minute {
		t.Fatalf("повтор оплаты продлил дважды: срок %v", got.ExpiresAt)
	}
}

// Сброс трафика должен дойти до ноды: она считает своим счётчиком, и без
// поправки исчерпавший квоту так и остался бы выключенным.
func TestTrafficResetLetsNodeServeTheFullLimitAgain(t *testing.T) {
	h := newHarness(t)
	node := h.createNode("fi-1")
	created := h.createUser(1000)
	account := h.nodeUsers(node.Token)[0].AccountID()
	h.do(http.MethodPost, "/api/v1/node/usage", node.Token,
		map[string]any{"usage": map[string]users.Usage{account: {Up: 400, Down: 600}}}, nil)
	// Свою тысячу нода уже насчитала сама и упирается в лимит 1000 своим
	// счётчиком; панель отдаёт ей лимит без вычета её же расхода.
	if got := h.nodeUsers(node.Token)[0].TrafficLimit; got != 1000 {
		t.Fatalf("до сброса нода получила лимит %d, ожидался 1000", got)
	}

	if code := h.do(http.MethodPost, "/api/v1/users/"+itoa(created.User.ID)+"/reset-traffic", adminToken, nil, nil); code != http.StatusOK {
		t.Fatalf("сброс трафика: код %d", code)
	}
	if used := h.getUser(created.User.ID).Used; used != 0 {
		t.Fatalf("после сброса расход %d", used)
	}
	// Нода уже насчитала 1000 своим счётчиком — значит, должна пустить ещё на 1000.
	got := h.nodeUsers(node.Token)[0]
	if !got.Enabled || got.TrafficLimit != 2000 {
		t.Fatalf("нода после сброса: включён %v, лимит %d, ожидался 2000", got.Enabled, got.TrafficLimit)
	}
}

func TestBotKeyReadsPlansButCannotChangeThem(t *testing.T) {
	h := newHarness(t)
	month(h)
	var key struct {
		Secret string `json:"secret"`
	}
	if code := h.do(http.MethodPost, "/api/v1/keys", adminToken,
		map[string]any{"name": "бот", "scopes": []string{"users"}}, &key); code != http.StatusOK {
		t.Fatalf("ключ: код %d", code)
	}
	var list struct {
		Plans []panel.Plan `json:"plans"`
	}
	if code := h.do(http.MethodGet, "/api/v1/plans", key.Secret, nil, &list); code != http.StatusOK || len(list.Plans) != 1 {
		t.Fatalf("бот не видит тарифы: код %d, тарифов %d", code, len(list.Plans))
	}
	if code := h.do(http.MethodPost, "/api/v1/plans", key.Secret, map[string]any{"name": "Даром", "days": 3650}, nil); code != http.StatusForbidden {
		t.Fatalf("бот завёл тариф: код %d", code)
	}
}

// Удалённый тариф не трогает тех, кто по нему уже купил: срок и лимиты у них
// свои, тариф — только шаблон.
func TestDeletingPlanLeavesItsClientsAsTheyAre(t *testing.T) {
	h := newHarness(t)
	plan := month(h)
	var created createUserResponse
	h.do(http.MethodPost, "/api/v1/users", adminToken, map[string]any{"label": "Ева", "plan_id": plan.ID}, &created)

	if code := h.do(http.MethodDelete, "/api/v1/plans/"+itoa(plan.ID), adminToken, nil, nil); code != http.StatusOK && code != http.StatusNoContent {
		t.Fatalf("удаление тарифа: код %d", code)
	}
	got := h.getUser(created.User.ID)
	if got.PlanID != nil || got.TrafficLimit != 50*gb || got.ExpiresAt == nil {
		t.Fatalf("покупатель изменился с тарифом: тариф %v, лимит %d, срок %v", got.PlanID, got.TrafficLimit, got.ExpiresAt)
	}
}

func TestPlanNeedsNameAndSaneNumbers(t *testing.T) {
	h := newHarness(t)
	for _, bad := range []map[string]any{
		{"name": "", "days": 30},
		{"name": "Минус", "days": -1},
		{"name": "Минус ГБ", "days": 30, "traffic_limit": -5},
	} {
		if code := h.do(http.MethodPost, "/api/v1/plans", adminToken, bad, nil); code != http.StatusBadRequest {
			t.Errorf("тариф %v принят: код %d", bad, code)
		}
	}
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func TestPanelDrawsQRForLinkButNotForHugeText(t *testing.T) {
	h := newHarness(t)
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/v1/qr", strings.NewReader(`{"text":"https://panel.example.com/sub/abc"}`))
	req.Header.Set("Authorization", "Bearer "+adminToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), "<svg") {
		t.Fatalf("QR-код: код %d, начало %.40q", resp.StatusCode, body)
	}
	if !strings.Contains(resp.Header.Get("Cache-Control"), "no-store") {
		t.Fatal("QR-код с доступом не должен кешироваться")
	}
	if code := h.do(http.MethodPost, "/api/v1/qr", adminToken, map[string]any{"text": strings.Repeat("a", 5000)}, nil); code != http.StatusBadRequest {
		t.Fatalf("огромный текст в QR: код %d", code)
	}
}
