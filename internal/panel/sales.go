package panel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Книга продаж.
//
// Продавцу нужен ответ на «сколько я заработал за месяц», а панель до сих пор
// знала только трафик. Деньги принимает не она — бот или сам продавец, — но
// каждая оплата кончается здесь: заведением покупателя по тарифу или
// продлением по тарифу. Значит, цена тарифа в момент такого действия и есть
// выручка, и считать её можно, не трогая ни одной платёжной системы.
//
// Чем книга намеренно НЕ является.
//
// Это не список покупок покупателей. В строке нет ни покупателя, ни времени
// точнее дня: «в такой-то день продан Месяц за 300 ₽». Удалили человека —
// его покупки остаются цифрой в выручке, но связать их с ним уже нечем, и
// «удалили значит удалили» остаётся правдой. Со временем до секунды строку
// можно было бы сопоставить с журналом действий, пока тот хранится; день
// такого не даёт.
//
// И это не бухгалтерия. Возвраты, скидки, оплата мимо тарифа сюда не
// попадают; панель показывает «по тарифам», и подписи в интерфейсе так и
// говорят.

// SalesKeepDays — сколько дней держим книгу. Как у истории трафика: год с
// запасом, чтобы сравнить месяц с тем же месяцем прошлого года.
const SalesKeepDays = UsageKeepDays

// Виды продаж.
const (
	SaleNew   = "new"
	SaleRenew = "renew"
)

// recordSale пишет продажу в той же транзакции, что и само действие: доступ
// выдан — продажа учтена, не выдан — не учтена. Бесплатные тарифы тоже
// пишем: «сколько раз продлили» продавцу нужно и без цены.
func recordSale(ctx context.Context, tx *sql.Tx, kind string, plan Plan, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO sales (day, kind, plan_id, plan_name, price, currency) VALUES (?, ?, ?, ?, ?, ?)`,
		now.UTC().Format("2006-01-02"), kind, plan.ID, plan.Name, plan.Price, plan.Currency); err != nil {
		return fmt.Errorf("запись продажи: %w", err)
	}
	return nil
}

// Money — выручка в одной валюте за период и за такой же период перед ним.
//
// По валютам отдельно: сложить рубли с долларами значило бы показать число,
// которого нет.
type Money struct {
	Currency string `json:"currency"`
	Sum      int64  `json:"sum"`
	Prev     int64  `json:"prev"`
	Sales    int    `json:"sales"`
	New      int    `json:"new"`
	Renew    int    `json:"renew"`
}

// PlanSales — сколько продано по тарифу за период.
type PlanSales struct {
	PlanID   int64  `json:"plan_id,omitempty"`
	Name     string `json:"name"`
	Currency string `json:"currency"`
	Count    int    `json:"count"`
	Sum      int64  `json:"sum"`
}

// DaySales — выручка за день, для графика.
type DaySales struct {
	Day      string `json:"day"`
	Currency string `json:"currency"`
	Sum      int64  `json:"sum"`
	Count    int    `json:"count"`
}

// SalesReport — всё о продажах за период.
type SalesReport struct {
	Money  []Money     `json:"money"`
	ByPlan []PlanSales `json:"by_plan"`
	ByDay  []DaySales  `json:"by_day"`
}

// Sales собирает выручку за последние days дней и за столько же перед ними.
func (s *Store) Sales(ctx context.Context, days int) (SalesReport, error) {
	if days <= 0 {
		days = 30
	}
	now := time.Now().UTC()
	from := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	prevFrom := now.AddDate(0, 0, -(2*days - 1)).Format("2006-01-02")

	report := SalesReport{Money: []Money{}, ByPlan: []PlanSales{}, ByDay: []DaySales{}}

	rows, err := s.db.QueryContext(ctx, `
		SELECT currency,
		       COALESCE(SUM(CASE WHEN day >= ?1 THEN price END), 0),
		       COALESCE(SUM(CASE WHEN day <  ?1 THEN price END), 0),
		       COUNT(CASE WHEN day >= ?1 THEN 1 END),
		       COUNT(CASE WHEN day >= ?1 AND kind = 'new' THEN 1 END),
		       COUNT(CASE WHEN day >= ?1 AND kind = 'renew' THEN 1 END)
		FROM sales WHERE day >= ?2
		GROUP BY currency
		ORDER BY 2 DESC, 4 DESC`, from, prevFrom)
	if err != nil {
		return report, fmt.Errorf("чтение выручки: %w", err)
	}
	for rows.Next() {
		var m Money
		if err := rows.Scan(&m.Currency, &m.Sum, &m.Prev, &m.Sales, &m.New, &m.Renew); err != nil {
			rows.Close()
			return report, err
		}
		report.Money = append(report.Money, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report, err
	}

	// По имени на день продажи, а не по нынешнему: тариф могли переименовать
	// или удалить, а продан он был под тем именем.
	rows, err = s.db.QueryContext(ctx, `
		SELECT COALESCE(plan_id, 0), plan_name, currency, COUNT(*), SUM(price)
		FROM sales WHERE day >= ?
		GROUP BY COALESCE(plan_id, 0), plan_name, currency
		ORDER BY SUM(price) DESC, COUNT(*) DESC`, from)
	if err != nil {
		return report, fmt.Errorf("продажи по тарифам: %w", err)
	}
	for rows.Next() {
		var p PlanSales
		if err := rows.Scan(&p.PlanID, &p.Name, &p.Currency, &p.Count, &p.Sum); err != nil {
			rows.Close()
			return report, err
		}
		report.ByPlan = append(report.ByPlan, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return report, err
	}

	rows, err = s.db.QueryContext(ctx, `
		SELECT day, currency, SUM(price), COUNT(*)
		FROM sales WHERE day >= ?
		GROUP BY day, currency ORDER BY day`, from)
	if err != nil {
		return report, fmt.Errorf("выручка по дням: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var d DaySales
		if err := rows.Scan(&d.Day, &d.Currency, &d.Sum, &d.Count); err != nil {
			return report, err
		}
		report.ByDay = append(report.ByDay, d)
	}
	return report, rows.Err()
}

// ForgetOldSales выбрасывает книгу старше keep дней.
func (s *Store) ForgetOldSales(ctx context.Context, keep int) error {
	if keep <= 0 {
		keep = SalesKeepDays
	}
	edge := time.Now().UTC().AddDate(0, 0, -keep).Format("2006-01-02")
	if _, err := s.db.ExecContext(ctx, `DELETE FROM sales WHERE day < ?`, edge); err != nil {
		return fmt.Errorf("уборка книги продаж: %w", err)
	}
	return nil
}

// cleanCurrency — знак валюты: короткий, без пробелов по краям. Пустой — та
// же валюта, что «без знака»: продавец, торгующий в одной валюте, может её
// и не писать.
func cleanCurrency(c string) string { return strings.TrimSpace(c) }
