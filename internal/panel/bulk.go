package panel

import (
	"encoding/csv"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// maxBulk — сколько покупателей за одно действие. Больше — уже не «продлить
// пострадавших», а повод разбить на части, чем держать базу одной правкой.
const maxBulk = 5000

// bulkUsers делает одно действие над списком покупателей.
//
// Нужно после аварии и для рутины: «продлить всем, кто истекает на этой
// неделе», «отключить не заплативших». По одному это сотня нажатий. Удаления
// здесь нет намеренно: стереть сотню покупателей одним запросом — слишком
// дорогая опечатка, а отключение обратимо.
//
// Каждый покупатель — отдельная правка: не вышло у одного (удалён, пока
// продавец выбирал), остальные всё равно сделаны, а не вышедшие перечислены.
func (a *API) bulkUsers(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs      []int64 `json:"ids"`
		Action   string  `json:"action"`
		PlanID   int64   `json:"plan_id"`
		ExtendBy string  `json:"extend_by"`
	}
	if !decode(w, r, &body) {
		return
	}
	if len(body.IDs) == 0 || len(body.IDs) > maxBulk {
		fail(w, http.StatusBadRequest, "нужен список от 1 до 5000 покупателей")
		return
	}

	var apply func(id int64) error
	var detail string
	switch body.Action {
	case "renew":
		plan, err := a.store.GetPlan(r.Context(), body.PlanID)
		if err != nil {
			fail(w, http.StatusBadRequest, "не указан тариф или его нет: plan_id")
			return
		}
		detail = "массово по тарифу «" + plan.Name + "»"
		apply = func(id int64) error { _, err := a.store.RenewUser(r.Context(), id, plan.ID); return err }
	case "extend":
		d, err := ParseDuration(body.ExtendBy)
		if err != nil || d <= 0 {
			fail(w, http.StatusBadRequest, "срок продления: extend_by вида 30d")
			return
		}
		detail = "массово продлён на " + body.ExtendBy
		apply = func(id int64) error { _, err := a.store.ExtendUser(r.Context(), id, d); return err }
	case "enable", "disable":
		on := body.Action == "enable"
		detail = map[bool]string{true: "массово включён", false: "массово отключён"}[on]
		apply = func(id int64) error {
			_, err := a.store.UpdateUser(r.Context(), id, UpdateUserParams{Enabled: &on})
			return err
		}
	case "reset":
		detail = "массово обнулён расход"
		apply = func(id int64) error { _, err := a.store.ResetTraffic(r.Context(), id); return err }
	default:
		fail(w, http.StatusBadRequest, "действие — renew, extend, enable, disable или reset")
		return
	}

	// Повтор с тем же ключом — двойной клик или переотправка — второй раз
	// не продлевает.
	if handled := a.replayed(w, r, "bulk"); handled {
		return
	}

	failed := []int64{}
	done := 0
	for _, id := range body.IDs {
		if err := apply(id); err != nil {
			failed = append(failed, id)
			continue
		}
		done++
		a.record(r, EventUserUpdate, Event{UserID: id, Detail: detail})
	}
	answer := map[string]any{"done": done, "failed": failed}
	a.remember(r, "bulk", 0, answer)
	ok(w, answer)
}

// usersCSV выгружает покупателей таблицей — для учёта, бухгалтерии и
// переезда. Секретов в ней нет: ни токенов подписки, ни ключей. Выгрузка —
// это список, а не способ унести доступы.
func (a *API) usersCSV(w http.ResponseWriter, r *http.Request) {
	list, err := a.store.ListUsers(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	plans, _ := a.store.ListPlans(r.Context())
	planName := map[int64]string{}
	for _, p := range plans {
		planName[p.ID] = p.Name
	}

	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="marvia-clients.csv"`)
	w.Header().Set("Cache-Control", "no-store")
	// Метка порядка байтов: без неё Excel открывает UTF-8 как кракозябры.
	_, _ = w.Write([]byte{0xEF, 0xBB, 0xBF})
	out := csv.NewWriter(w)
	_ = out.Write([]string{"id", "label", "external_id", "enabled", "expires_at", "plan", "traffic_limit", "used", "max_ips", "created_at", "last_seen"})
	when := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	for _, u := range list {
		plan := ""
		if u.PlanID != nil {
			plan = planName[*u.PlanID]
		}
		_ = out.Write([]string{
			strconv.FormatInt(u.ID, 10), cell(u.Label), cell(u.ExternalID), strconv.FormatBool(u.Enabled),
			when(u.ExpiresAt), cell(plan), strconv.FormatInt(u.TrafficLimit, 10), strconv.FormatInt(u.Used, 10),
			strconv.Itoa(u.MaxIPs), when(&u.CreatedAt), when(u.LastSeen),
		})
	}
	out.Flush()
}

// cell не даёт тексту продавца стать формулой в Excel и LibreOffice: строка,
// начинающаяся с =, +, - или @, там исполняется, и метка «=HYPERLINK(…)»
// превратилась бы в ссылку. Апостроф впереди — общепринятый способ сказать
// «это текст».
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}
