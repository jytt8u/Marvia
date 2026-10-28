package panel

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BackupSettings хранит получателя шифрования, но не пароль и не приватный
// ключ. Даже восстановленная база не должна давать ключ к соседним копиям.
type BackupSettings struct {
	Enabled   bool             `json:"enabled"`
	Time      string           `json:"time"`
	Keep      int              `json:"keep"`
	Telegram  bool             `json:"telegram"`
	S3        S3BackupSettings `json:"s3"`
	PublicKey []byte           `json:"public_key,omitempty"`
	Salt      []byte           `json:"salt,omitempty"`
}

func (c BackupSettings) hasPassword() bool {
	return len(c.PublicKey) == 32 && len(c.Salt) == backupSaltSize
}

func (s *Store) BackupSettings(ctx context.Context) (BackupSettings, error) {
	cfg := BackupSettings{Time: "03:00", Keep: BackupKeep}
	err := s.readBackupSetting(ctx, "scheduled-backup", &cfg)
	return cfg, err
}

func (s *Store) readBackupSetting(ctx context.Context, key string, out any) error {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("чтение настроек копий: %w", err)
	}
	if err := json.Unmarshal([]byte(raw), out); err != nil {
		return errors.New("настройки копий повреждены")
	}
	return nil
}

func (s *Store) writeBackupSetting(ctx context.Context, key string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, string(raw))
	if err != nil {
		return fmt.Errorf("сохранение настроек копий: %w", err)
	}
	return nil
}

// SetBackupSettings не позволяет включить отправку без пароля даже через API.
// Пустой пароль сохраняет прежнего получателя; смена не открывает старые копии.
func (s *Store) SetBackupSettings(ctx context.Context, cfg BackupSettings, password string) error {
	current, err := s.BackupSettings(ctx)
	if err != nil {
		return err
	}
	cfg.PublicKey, cfg.Salt = current.PublicKey, current.Salt
	if password != "" {
		cfg.PublicKey, cfg.Salt, err = PasswordBackupRecipient(password)
		if err != nil {
			return err
		}
	}
	if _, err := time.Parse("15:04", cfg.Time); err != nil || len(cfg.Time) != 5 {
		return errors.New("время копии нужно в формате ЧЧ:ММ по UTC")
	}
	if cfg.Keep < 1 || cfg.Keep > 365 {
		return errors.New("число копий должно быть от 1 до 365")
	}
	if cfg.S3.SecretKey == "" {
		cfg.S3.SecretKey = current.S3.SecretKey
	}
	if cfg.S3.SessionToken == "" {
		cfg.S3.SessionToken = current.S3.SessionToken
	}
	if cfg.Enabled {
		if !cfg.hasPassword() {
			return errors.New("без пароля расписание копий не включается")
		}
		if !cfg.Telegram && !cfg.S3.Enabled {
			return errors.New("выбери Telegram или S3 для копий")
		}
		alerts, err := s.AlertSettings(ctx)
		if err != nil {
			return err
		}
		// Без работающего канала оповещений сбой S3 снова остался бы незаметным.
		if !alerts.Ready() {
			return errors.New("сначала включи оповещения и задай бота и чат")
		}
		if cfg.S3.Enabled {
			if err := cfg.S3.validate(); err != nil {
				return err
			}
		}
	}
	return s.writeBackupSetting(ctx, "scheduled-backup", cfg)
}

type telegramBackupSlot struct {
	MessageID int64     `json:"message_id"`
	UpdatedAt time.Time `json:"updated_at"`
}

type backupRunState struct {
	AttemptDay  string    `json:"attempt_day"`
	LastSuccess time.Time `json:"last_success"`
	LastError   string    `json:"last_error"`
	// Токен не дублируем: смена бота определяется его открытым номером.
	BotID  string               `json:"bot_id"`
	ChatID string               `json:"chat_id"`
	Slots  []telegramBackupSlot `json:"slots"`
}

// ScheduledBackups сериализует запуск: опрос и ручная копия не должны
// перезаписывать одни и те же сообщения или удалять копии друг друга.
type ScheduledBackups struct {
	store          *Store
	alerts         *Alerts
	dir            string
	mu             sync.Mutex
	sendDocument   func(context.Context, AlertSettings, string, int64) (int64, error)
	deleteDocument func(context.Context, AlertSettings, int64) error
	s3Client       *http.Client
}

func NewScheduledBackups(store *Store, alerts *Alerts, dir string) *ScheduledBackups {
	return &ScheduledBackups{store: store, alerts: alerts, dir: dir,
		sendDocument: sendBackupDocument, deleteDocument: deleteBackupDocument,
		s3Client: &http.Client{Timeout: 5 * time.Minute, CheckRedirect: refuseBackupRedirect}}
}

func refuseBackupRedirect(_ *http.Request, _ []*http.Request) error {
	// Секреты и подпись предназначены одному хосту, а не его перенаправлению.
	return http.ErrUseLastResponse
}

func (b *ScheduledBackups) Watch(ctx context.Context, notify func(error)) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		if err := b.Check(ctx, time.Now()); err != nil && notify != nil {
			notify(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check догоняет сегодняшнюю копию после простоя, но помнит попытку в базе:
// перезапуск в ту же ночь не отправляет десятки одинаковых документов.
func (b *ScheduledBackups) Check(ctx context.Context, now time.Time) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cfg, err := b.store.BackupSettings(ctx)
	if err != nil {
		return b.report(ctx, err)
	}
	if !cfg.Enabled {
		return nil
	}
	var state backupRunState
	if err := b.store.readBackupSetting(ctx, "scheduled-backup-state", &state); err != nil {
		return b.report(ctx, err)
	}
	now = now.UTC()
	day := now.Format("2006-01-02")
	at, err := time.Parse("2006-01-02 15:04", day+" "+cfg.Time)
	if err != nil {
		return b.report(ctx, errors.New("время расписания копий повреждено"))
	}
	if now.Before(at) || state.AttemptDay >= day {
		return nil
	}
	state.AttemptDay = day
	if err := b.store.writeBackupSetting(ctx, "scheduled-backup-state", state); err != nil {
		return b.report(ctx, err)
	}
	return b.run(ctx, cfg, &state, now)
}

func (b *ScheduledBackups) RunNow(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	cfg, err := b.store.BackupSettings(ctx)
	if err != nil {
		return b.report(ctx, err)
	}
	if !cfg.Enabled {
		return errors.New("расписание копий выключено")
	}
	var state backupRunState
	if err := b.store.readBackupSetting(ctx, "scheduled-backup-state", &state); err != nil {
		return b.report(ctx, err)
	}
	return b.run(ctx, cfg, &state, time.Now().UTC())
}

func (b *ScheduledBackups) run(ctx context.Context, cfg BackupSettings, state *backupRunState, now time.Time) error {
	err := b.deliver(ctx, cfg, state, now)
	state.LastError = ""
	if err != nil {
		state.LastError = err.Error()
	} else {
		state.LastSuccess = now
	}
	// Независимый контекст сохраняет исход даже при отмене ручного HTTP-запроса.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if saveErr := b.store.writeBackupSetting(saveCtx, "scheduled-backup-state", state); saveErr != nil {
		err = errors.Join(err, saveErr)
	}
	return b.report(saveCtx, err)
}

func (b *ScheduledBackups) report(ctx context.Context, err error) error {
	if b.alerts != nil {
		b.alerts.RemoteBackupFailed(ctx, err)
	}
	return err
}

func (b *ScheduledBackups) deliver(ctx context.Context, cfg BackupSettings, state *backupRunState, now time.Time) error {
	// Второй барьер защищает от повреждённой записи и прямого вызова рабочего
	// цикла: открытый файл ни при каких обстоятельствах не выходит наружу.
	if !cfg.hasPassword() {
		return errors.New("без пароля копия не отправляется")
	}
	if cfg.Keep < 1 || (!cfg.Telegram && !cfg.S3.Enabled) {
		return errors.New("настройки отправки копий повреждены")
	}
	alerts, err := b.store.AlertSettings(ctx)
	if err != nil {
		return err
	}
	if !alerts.Ready() {
		return errors.New("оповещения для копий выключены")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	// VACUUM требует открытый файл. Он живёт только в закрытом временном
	// каталоге и убирается также при ошибке шифрования или отмене запуска.
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return errors.New("не удалось создать каталог копий")
	}
	tmp, err := os.MkdirTemp(b.dir, ".scheduled-*")
	if err != nil {
		return errors.New("не удалось создать временный каталог копии")
	}
	defer os.RemoveAll(tmp)
	name := "panel-" + now.Format("2006-01-02-150405.000000000") + ".db"
	plainPath := filepath.Join(tmp, name)
	if err := b.store.Backup(ctx, plainPath); err != nil {
		return fmt.Errorf("снимок базы не снялся: %w", err)
	}
	sealed, err := sealPasswordBackup(plainPath, cfg.PublicKey, cfg.Salt)
	if err != nil {
		return fmt.Errorf("шифрование копии: %w", err)
	}
	path := filepath.Join(b.dir, name+sealedSuffix)
	if err := os.Rename(sealed, path); err != nil {
		return errors.New("не удалось сохранить зашифрованную копию")
	}
	var failures []error
	if cfg.Telegram {
		if err := b.deliverTelegram(ctx, alerts, path, cfg.Keep, state, now); err != nil {
			failures = append(failures, fmt.Errorf("отправка в Telegram: %w", err))
		}
	}
	// Оба назначения пробуем независимо: недоступный Telegram не должен
	// лишать продавца копии в S3, а успех S3 не должен скрывать сбой Telegram.
	if cfg.S3.Enabled {
		if err := cfg.S3.uploadAndPrune(ctx, b.s3Client, path, cfg.Keep); err != nil {
			failures = append(failures, fmt.Errorf("отправка в S3: %w", err))
		}
	}
	// У расписания свой подкаталог: N не сокращает прежние локальные копии.
	if err := pruneBackups(b.dir, cfg.Keep); err != nil {
		failures = append(failures, err)
	}
	return errors.Join(failures...)
}

func (b *ScheduledBackups) deliverTelegram(ctx context.Context, cfg AlertSettings, path string, keep int, state *backupRunState, now time.Time) error {
	botID := strings.SplitN(cfg.BotToken, ":", 2)[0]
	if state.BotID != botID || state.ChatID != cfg.ChatID {
		state.BotID, state.ChatID, state.Slots = botID, cfg.ChatID, nil
	}
	index := -1
	if len(state.Slots) >= keep {
		index = 0
		for i := range state.Slots {
			if state.Slots[i].UpdatedAt.Before(state.Slots[index].UpdatedAt) {
				index = i
			}
		}
	}
	var messageID int64
	if index >= 0 {
		messageID = state.Slots[index].MessageID
	}
	id, err := b.sendDocument(ctx, cfg, path, messageID)
	if err != nil {
		return err
	}
	slot := telegramBackupSlot{MessageID: id, UpdatedAt: now}
	if index >= 0 {
		state.Slots[index] = slot
	} else {
		state.Slots = append(state.Slots, slot)
	}
	// Замена документа работает и через неделю, в отличие от deleteMessage.
	// При уменьшении N старые сообщения могут быть старше 48 часов: отказ
	// удаления честно станет ошибкой, вместо обещания несуществующей очистки.
	for len(state.Slots) > keep {
		index = 0
		for i := range state.Slots {
			if state.Slots[i].UpdatedAt.Before(state.Slots[index].UpdatedAt) {
				index = i
			}
		}
		if err := b.deleteDocument(ctx, cfg, state.Slots[index].MessageID); err != nil {
			return fmt.Errorf("старое сообщение не удалено; удали его вручную: %w", err)
		}
		state.Slots = append(state.Slots[:index], state.Slots[index+1:]...)
	}
	return nil
}

func (a *API) getBackupSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := a.store.BackupSettings(r.Context())
	if err != nil {
		respondStoreErr(w, err)
		return
	}
	var state backupRunState
	if err := a.store.readBackupSetting(r.Context(), "scheduled-backup-state", &state); err != nil {
		respondStoreErr(w, err)
		return
	}
	s3 := cfg.S3
	hasSecret, hasSession := s3.SecretKey != "", s3.SessionToken != ""
	s3.SecretKey, s3.SessionToken = "", ""
	ok(w, map[string]any{"enabled": cfg.Enabled, "time": cfg.Time, "keep": cfg.Keep,
		"telegram": cfg.Telegram, "s3": s3, "has_password": cfg.hasPassword(),
		"has_s3_secret": hasSecret, "has_s3_session": hasSession,
		"last_success": state.LastSuccess, "last_error": state.LastError})
}

func (a *API) setBackupSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Enabled  bool             `json:"enabled"`
		Time     string           `json:"time"`
		Keep     int              `json:"keep"`
		Telegram bool             `json:"telegram"`
		S3       S3BackupSettings `json:"s3"`
		Password string           `json:"password"`
	}
	if !decode(w, r, &body) {
		return
	}
	cfg := BackupSettings{Enabled: body.Enabled, Time: body.Time, Keep: body.Keep, Telegram: body.Telegram, S3: body.S3}
	if err := a.store.SetBackupSettings(r.Context(), cfg, body.Password); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	a.record(r, EventAlertsUpdate, Event{Detail: "расписание резервных копий изменено"})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) runBackup(w http.ResponseWriter, r *http.Request) {
	if a.scheduledBackups == nil {
		fail(w, http.StatusServiceUnavailable, "отправка копий не запущена")
		return
	}
	if err := a.scheduledBackups.RunNow(r.Context()); err != nil {
		fail(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) WithScheduledBackups(b *ScheduledBackups) *API {
	a.scheduledBackups = b
	return a
}
