package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Ночное автообновление.
//
// Продавец узнавал о новом выпуске, открыв «Обновления», и обновлял руками —
// то есть обновлял, когда вспоминал. А вспоминают, когда что-то сломалось.
// Здесь панель делает то же самое сама: в выбранный час проверяет свой канал
// и, если вышло новее, просит отставшие ноды и службу на своей машине. Ставит
// всё та же служба обновления (internal/updater): официальный релиз, сверенный
// с подписью, и возврат прежней версии, если новая не поднялась.
//
// Ночью — потому что ноды перезапускаются на секунды, а панель — на полминуты:
// в четыре утра это заденет меньше всего покупателей. Час по Москве: продавцы
// и покупатели у Marvia в основном там, а у сервера часовой пояс бывает любой.

// ActorAuto — от чьего имени автообновление пишет в журнал.
const ActorAuto = "auto"

// autoEvery — как часто проверять, не настал ли час. Десять минут: в час
// окна проверка случится шесть раз, и хоть одна застанет сервер живым.
var autoEvery = 10 * time.Minute

// moscow — часовой пояс расписания. FixedZone, а не LoadLocation: на
// минимальном сервере может не быть базы часовых поясов, а Москва живёт без
// перевода часов с 2014 года.
var moscow = time.FixedZone("MSK", 3*60*60)

// AutoUpdateSettings — включено ли автообновление и в какой час по Москве.
type AutoUpdateSettings struct {
	Enabled bool `json:"enabled"`
	Hour    int  `json:"hour"`
}

// defaultAutoHour — четыре утра: позже всех ночных сов и раньше всех жаворонков.
const defaultAutoHour = 4

// AutoUpdateSettings читает настройку; по умолчанию выключено, в четыре утра.
func (s *Store) AutoUpdateSettings(ctx context.Context) (AutoUpdateSettings, error) {
	out := AutoUpdateSettings{Hour: defaultAutoHour}
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'autoupdate'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, fmt.Errorf("чтение автообновления: %w", err)
	}
	// Испорченная запись — выключено: обновлять сервер по мусору нельзя.
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out.Hour < 0 || out.Hour > 23 {
		return AutoUpdateSettings{Hour: defaultAutoHour}, nil
	}
	return out, nil
}

// SetAutoUpdateSettings сохраняет настройку.
func (s *Store) SetAutoUpdateSettings(ctx context.Context, v AutoUpdateSettings) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO settings (key, value) VALUES ('autoupdate', ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, string(raw))
	if err != nil {
		return fmt.Errorf("сохранение автообновления: %w", err)
	}
	return nil
}

// autoState — что автообновление уже сделало: один и тот же выпуск дважды
// за ночь не просим, иначе каждая из шести проверок часа клала бы просьбу,
// а служба на каждую качала бы приложения заново.
type autoState struct {
	mu   sync.Mutex
	done map[string]string // выпуск → день по Москве, когда просили
}

// KeepUpToDate проверяет раз в autoEvery, не пора ли обновиться, пока не
// отменят контекст.
func (a *API) KeepUpToDate(ctx context.Context) {
	t := time.NewTicker(autoEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if err := a.autoUpdateAt(ctx, now); err != nil {
				log.Printf("автообновление: %v", err)
			}
		}
	}
}

// autoUpdateAt — одна проверка в момент now. Отдельно от цикла ради тестов:
// ждать четырёх утра в тесте никто не станет.
func (a *API) autoUpdateAt(ctx context.Context, now time.Time) error {
	cfg, err := a.store.AutoUpdateSettings(ctx)
	if err != nil || !cfg.Enabled {
		return err
	}
	local := now.In(moscow)
	if local.Hour() != cfg.Hour {
		return nil
	}
	latest, _ := a.releases.get(ctx, false)
	if latest == "" {
		return nil
	}
	day := local.Format("2006-01-02")
	a.auto.mu.Lock()
	if a.auto.done == nil {
		a.auto.done = map[string]string{}
	}
	if a.auto.done[latest] == day {
		a.auto.mu.Unlock()
		return nil
	}
	a.auto.done[latest] = day
	a.auto.mu.Unlock()

	res, err := a.upgradeEverything(ctx, ActorAuto)
	if err != nil {
		return err
	}
	if res.Asked > 0 || res.Panel {
		log.Printf("автообновление до %s: нод %d, панель %v", res.Release, res.Asked, res.Panel)
	}
	return nil
}

// getAutoUpdate и setAutoUpdate — переключатель на странице «Обновления».
func (a *API) getAutoUpdate(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.store.AutoUpdateSettings(r.Context())
	if err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	ok(w, cfg)
}

func (a *API) setAutoUpdate(w http.ResponseWriter, r *http.Request) {
	var in AutoUpdateSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&in); err != nil {
		fail(w, http.StatusBadRequest, "не разобрал настройку")
		return
	}
	if in.Hour < 0 || in.Hour > 23 {
		fail(w, http.StatusBadRequest, "час — от 0 до 23")
		return
	}
	if err := a.store.SetAutoUpdateSettings(r.Context(), in); err != nil {
		fail(w, http.StatusInternalServerError, err.Error())
		return
	}
	detail := "выключено"
	if in.Enabled {
		detail = fmt.Sprintf("каждую ночь в %02d:00 по Москве", in.Hour)
	}
	a.record(r, EventAutoUpdate, Event{Detail: detail})
	ok(w, in)
}
