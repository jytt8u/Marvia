package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/jytt8u/marvia/internal/seller"
)

// Связь с покупателем: ссылка поддержки, ссылка «Продлить» и объявление.
//
// Покупателю, у которого кончился срок или пропала связь, должно быть куда
// нажать. Чужие приложения (Happ, v2RayTun, Hiddify) рисуют такие кнопки по
// заголовкам подписки, наш клиент получает то же полями в JSON. Правила
// проверки — в internal/seller, общие с клиентом.
//
// «Продлить» ведёт на страницу или в бота продавца. Платёжного шлюза у
// проекта нет и не будет — почему, сказано в конце README.

// SellerInfo читает настройки связи с покупателем. Пока продавец их не
// задал — всё пусто, и заголовков подписка не несёт.
func (s *Store) SellerInfo(ctx context.Context) (seller.Info, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'seller'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return seller.Info{}, nil
	}
	if err != nil {
		return seller.Info{}, fmt.Errorf("чтение настроек связи с покупателем: %w", err)
	}
	var out seller.Info
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		// Испорченная запись не должна ронять выдачу подписок: кнопки просто
		// пропадут, и продавец задаст их заново.
		return seller.Info{}, nil
	}
	// Чистим и прочитанное. Запись могла попасть в базу в обход API —
	// восстановлением копии или руками, — а уходит она каждому покупателю.
	return seller.Clean(out), nil
}

// SetSellerInfo сохраняет настройки связи с покупателем. Проверка — дело
// вызывающего: здесь кладётся то, что уже прошло seller.Info.Check.
func (s *Store) SetSellerInfo(ctx context.Context, v seller.Info) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES ('seller', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, string(raw))
	if err != nil {
		return fmt.Errorf("сохранение настроек связи с покупателем: %w", err)
	}
	return nil
}

// getSeller отдаёт ссылки и объявление.
func (a *API) getSeller(w http.ResponseWriter, r *http.Request) {
	info, err := a.store.SellerInfo(r.Context())
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	ok(w, info)
}

// setSeller сохраняет ссылки и объявление целиком: пустое поле — выключено.
//
// Целиком, а не по одному полю: форма в панели одна, и «не прислал поле»
// против «стёр поле» различать незачем — строгий разбор и так не даст
// ошибиться в имени.
func (a *API) setSeller(w http.ResponseWriter, r *http.Request) {
	var body seller.Info
	if !decode(w, r, &body) {
		return
	}
	next, err := body.Check()
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	current, err := a.store.SellerInfo(r.Context())
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	if err := a.store.SetSellerInfo(r.Context(), next); err != nil {
		respondStoreErr(w, err)
		return
	}
	if detail := sellerDetail(current, next); detail != "" {
		a.record(r, EventSellerUpdate, Event{Detail: detail})
	}
	ok(w, next)
}

// sellerDetail описывает правку словами. Сами ссылки в журнал не пишем:
// журнал пересылают, а «сменили ссылку продления» и так говорит, что
// смотреть.
func sellerDetail(was, now seller.Info) string {
	var parts []string
	say := func(what, before, after string) {
		switch {
		case before == after:
		case after == "":
			parts = append(parts, what+" убрана")
		case before == "":
			parts = append(parts, what+" задана")
		default:
			parts = append(parts, what+" изменена")
		}
	}
	say("ссылка поддержки", was.SupportURL, now.SupportURL)
	say("ссылка продления", was.RenewURL, now.RenewURL)
	switch {
	case was.Announce == now.Announce:
	case now.Announce == "":
		parts = append(parts, "объявление убрано")
	case was.Announce == "":
		parts = append(parts, "объявление задано")
	default:
		parts = append(parts, "объявление изменено")
	}
	return strings.Join(parts, ", ")
}
