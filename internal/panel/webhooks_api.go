package panel

import (
	"net/http"
	"time"
)

func (a *API) getWebhooks(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.store.WebhookSettings(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "не удалось прочитать настройки вебхуков")
		return
	}
	var pending int
	if err := a.store.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM webhook_queue WHERE deadline > ?`, format(time.Now().UTC())).Scan(&pending); err != nil {
		fail(w, http.StatusInternalServerError, "не удалось прочитать очередь вебхуков")
		return
	}
	ok(w, map[string]any{"enabled": cfg.Enabled, "urls": emptyIfNil(cfg.URLs), "has_secret": cfg.Secret != "", "pending": pending})
}

func (a *API) setWebhooks(w http.ResponseWriter, r *http.Request) {
	var cfg WebhookSettings
	if !decodeBody(w, r, &cfg, true) {
		return
	}
	current, err := a.store.WebhookSettings(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, "не удалось прочитать настройки вебхуков")
		return
	}
	// Как с токеном Telegram: пустое поле сохраняет секрет, которого
	// браузер не знает и не должен получать обратно из API.
	if cfg.Secret == "" {
		cfg.Secret = current.Secret
	}
	checked, err := cfg.check()
	if err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := a.store.SetWebhookSettings(r.Context(), checked); err != nil {
		fail(w, http.StatusInternalServerError, "не удалось сохранить настройки вебхуков")
		return
	}
	detail := "вебхуки выключены"
	if checked.Enabled {
		detail = "вебхуки включены"
	}
	a.record(r, EventWebhooksUpdate, Event{Detail: detail})
	a.getWebhooks(w, r)
}
