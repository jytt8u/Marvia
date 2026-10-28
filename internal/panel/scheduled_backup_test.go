package panel

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const scheduledTestPassword = "пароль продавца хранится отдельно от сервера"

func scheduledStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	if err := s.SetAlertSettings(context.Background(), AlertSettings{Enabled: true, BotToken: "42:секрет", ChatID: "123"}); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func scheduledConfig() BackupSettings {
	return BackupSettings{Enabled: true, Time: "03:00", Keep: 2, Telegram: true}
}

func TestBackupWithoutPasswordNeverLeavesTheServer(t *testing.T) {
	s, dir := scheduledStore(t)
	ctx := context.Background()
	if err := s.SetBackupSettings(ctx, scheduledConfig(), ""); err == nil {
		t.Fatal("расписание включилось без пароля")
	}
	// Проверяем и повреждённую запись в обход настроек: барьер у отправки
	// обязателен, иначе одна битая запись отправила бы всю базу открыто.
	if err := s.writeBackupSetting(ctx, "scheduled-backup", scheduledConfig()); err != nil {
		t.Fatal(err)
	}
	b := NewScheduledBackups(s, nil, filepath.Join(dir, "scheduled"))
	calls := 0
	b.sendDocument = func(context.Context, AlertSettings, string, int64) (int64, error) { calls++; return 1, nil }
	if err := b.Check(ctx, time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("попытка без пароля не отклонена")
	}
	if calls != 0 {
		t.Fatal("копия без пароля ушла в Telegram")
	}
	if _, err := os.Stat(b.dir); !os.IsNotExist(err) {
		t.Fatal("открытый снимок создан без пароля")
	}
}

func TestTheSameClientsAndNodesRestoreFromTheDeliveredBackup(t *testing.T) {
	s, dir := scheduledStore(t)
	ctx := context.Background()
	for _, label := range []string{"первый", "второй"} {
		if _, _, err := s.CreateUser(ctx, CreateUserParams{Label: label, ExternalID: "tg:" + label, Kinds: []string{"vp1", "vless", "trojan"}, TrafficLimit: 100000}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.CreateNode(ctx, CreateNodeParams{Name: "основная", Address: "example.com:443"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBackupSettings(ctx, scheduledConfig(), scheduledTestPassword); err != nil {
		t.Fatal(err)
	}
	var delivered []byte
	b := NewScheduledBackups(s, nil, filepath.Join(dir, "scheduled"))
	b.sendDocument = func(_ context.Context, _ AlertSettings, path string, _ int64) (int64, error) {
		var err error
		delivered, err = os.ReadFile(path)
		return 1, err
	}
	if err := b.RunNow(ctx); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(delivered, []byte("SQLite format 3")) || bytes.Contains(delivered, []byte(scheduledTestPassword)) {
		t.Fatal("секреты ушли открыто")
	}
	copyPath := filepath.Join(dir, "полученная.db.sealed")
	if err := os.WriteFile(copyPath, delivered, 0o600); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "восстановленная.db")
	report, err := VerifyPasswordBackup(ctx, copyPath, scheduledTestPassword, outPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Clients != 2 || report.Nodes != 1 {
		t.Fatalf("проверка потеряла записи: %+v", report)
	}
	restored, err := Open(outPath)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	wantUsers, _ := s.ListUsers(ctx)
	haveUsers, err := restored.ListUsers(ctx)
	if err != nil || !reflect.DeepEqual(wantUsers, haveUsers) {
		t.Fatalf("клиенты изменились после восстановления: %v", err)
	}
	wantNodes, _ := s.ListNodes(ctx)
	haveNodes, err := restored.ListNodes(ctx)
	if err != nil || !reflect.DeepEqual(wantNodes, haveNodes) {
		t.Fatalf("ноды изменились после восстановления: %v", err)
	}
	for _, user := range wantUsers {
		want, _ := s.credentials(ctx, user.ID)
		have, err := restored.credentials(ctx, user.ID)
		if err != nil || !reflect.DeepEqual(want, have) {
			t.Fatalf("ключи покупателя изменились: %v", err)
		}
	}
	var raw string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key = 'scheduled-backup'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, scheduledTestPassword) || strings.Contains(raw, `"password"`) || strings.Contains(raw, `"private"`) {
		t.Fatal("панель сохранила пароль или приватный ключ")
	}
	if _, err := VerifyPasswordBackup(ctx, copyPath, "другой пароль", ""); err == nil {
		t.Fatal("копия открылась другим паролем")
	}
	delivered[len(delivered)-1] ^= 1
	if err := os.WriteFile(copyPath, delivered, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyPasswordBackup(ctx, copyPath, scheduledTestPassword, ""); err == nil {
		t.Fatal("испорченная копия прошла проверку")
	}
}

func TestDeliveryFailureArrivesAsAnAlert(t *testing.T) {
	s, dir := scheduledStore(t)
	ctx := context.Background()
	if err := s.SetBackupSettings(ctx, scheduledConfig(), scheduledTestPassword); err != nil {
		t.Fatal(err)
	}
	alerts := NewAlerts(s)
	var messages []string
	alerts.SendWith(func(_ context.Context, _ AlertSettings, text string) error {
		messages = append(messages, text)
		return nil
	})
	b := NewScheduledBackups(s, alerts, filepath.Join(dir, "scheduled"))
	b.sendDocument = func(context.Context, AlertSettings, string, int64) (int64, error) {
		return 0, errors.New("нет сети")
	}
	if err := b.RunNow(ctx); err == nil {
		t.Fatal("сбой отправки остался незамечен")
	}
	if len(messages) != 1 || !strings.Contains(messages[0], "нет сети") || !strings.Contains(messages[0], "не доставлена") {
		t.Fatalf("сбой не пришёл оповещением: %v", messages)
	}
	b.sendDocument = func(context.Context, AlertSettings, string, int64) (int64, error) { return 1, nil }
	if err := b.RunNow(ctx); err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || !strings.Contains(messages[1], "снова доставляется") {
		t.Fatalf("восстановление отправки не сообщено: %v", messages)
	}
	// Ошибка VACUUM/каталога проходит тем же каналом, а не только в журнал.
	b.dir = filepath.Join(dir, "не каталог")
	if err := os.WriteFile(b.dir, []byte("занято"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.RunNow(ctx); err == nil || len(messages) != 3 {
		t.Fatalf("ошибка снимка не пришла: %v", messages)
	}
}

func TestTheDailyScheduleSurvivesRestartWithoutRepeatingTheBackup(t *testing.T) {
	s, dir := scheduledStore(t)
	ctx := context.Background()
	if err := s.SetBackupSettings(ctx, scheduledConfig(), scheduledTestPassword); err != nil {
		t.Fatal(err)
	}
	calls := 0
	send := func(context.Context, AlertSettings, string, int64) (int64, error) { calls++; return int64(calls), nil }
	b := NewScheduledBackups(s, nil, filepath.Join(dir, "scheduled"))
	b.sendDocument = send
	now := time.Date(2026, 10, 5, 2, 59, 0, 0, time.UTC)
	if err := b.Check(ctx, now); err != nil || calls != 0 {
		t.Fatal("копия ушла раньше времени")
	}
	if err := b.Check(ctx, now.Add(time.Minute)); err != nil || calls != 1 {
		t.Fatalf("копия не ушла в назначенное время: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(filepath.Join(dir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	b = NewScheduledBackups(reopened, nil, filepath.Join(dir, "scheduled"))
	b.sendDocument = send
	if err := b.Check(ctx, now.Add(2*time.Hour)); err != nil || calls != 1 {
		t.Fatal("перезапуск повторил сегодняшнюю копию")
	}
	if err := b.Check(ctx, now.Add(25*time.Hour)); err != nil || calls != 2 {
		t.Fatalf("следующие сутки не снялись после перезапуска: %v", err)
	}
}

func TestTelegramAndTheDiskKeepOnlyTheLatestCopies(t *testing.T) {
	s, dir := scheduledStore(t)
	ctx := context.Background()
	if err := s.SetBackupSettings(ctx, scheduledConfig(), scheduledTestPassword); err != nil {
		t.Fatal(err)
	}
	documents := make(map[int64][]byte)
	send := func(_ context.Context, _ AlertSettings, path string, id int64) (int64, error) {
		if id == 0 {
			id = int64(len(documents) + 1)
		}
		raw, err := os.ReadFile(path)
		documents[id] = raw
		return id, err
	}
	b := NewScheduledBackups(s, nil, filepath.Join(dir, "scheduled"))
	b.sendDocument = send
	for day := 5; day <= 9; day++ {
		if _, _, err := s.CreateUser(ctx, CreateUserParams{Label: "новый"}); err != nil {
			t.Fatal(err)
		}
		if err := b.Check(ctx, time.Date(2026, 10, day, 4, 0, 0, 0, time.UTC)); err != nil {
			t.Fatal(err)
		}
		// Новый наблюдатель должен продолжить прежние сообщения, а не создать
		// ещё два: номера сообщений хранятся в базе вместе с результатом.
		b = NewScheduledBackups(s, nil, b.dir)
		b.sendDocument = send
	}
	if len(documents) != 2 {
		t.Fatalf("в чате %d копий вместо двух", len(documents))
	}
	for _, raw := range documents {
		plain, err := OpenPasswordBackup(raw, scheduledTestPassword)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "panel.db")
		if err := os.WriteFile(path, plain, 0o600); err != nil {
			t.Fatal(err)
		}
		restored, err := Open(path)
		if err != nil {
			t.Fatal(err)
		}
		users, err := restored.ListUsers(ctx)
		_ = restored.Close()
		if err != nil || len(users) < 4 {
			t.Fatalf("в чате сохранилась старая копия: %v", err)
		}
	}
	files, err := os.ReadDir(b.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("на диске %d копий вместо двух", len(files))
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".db.sealed") {
			t.Fatal("на диске осталась открытая или временная копия")
		}
	}
}

func TestVerificationRejectsMissingSchemaInsteadOfRepairingIt(t *testing.T) {
	s, dir := scheduledStore(t)
	ctx := context.Background()
	if _, err := s.db.Exec("DROP TABLE events"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "сломанная.db")
	if err := s.Backup(ctx, path); err != nil {
		t.Fatal(err)
	}
	pub, salt, err := PasswordBackupRecipient(scheduledTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := sealPasswordBackup(path, pub, salt)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "не должна появиться.db")
	if _, err := VerifyPasswordBackup(ctx, sealed, scheduledTestPassword, out); err == nil || !strings.Contains(err.Error(), "схема") {
		t.Fatalf("неполная схема прошла проверку: %v", err)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("непроверенная база сохранена для восстановления")
	}
}

func TestAnUndeliveredFailureAlertIsRetried(t *testing.T) {
	s, _ := scheduledStore(t)
	alerts := NewAlerts(s)
	calls := 0
	alerts.SendWith(func(context.Context, AlertSettings, string) error {
		calls++
		if calls == 1 {
			return errors.New("нет сети")
		}
		return nil
	})
	alerts.RemoteBackupFailed(context.Background(), errors.New("S3 недоступен"))
	alerts.RemoteBackupFailed(context.Background(), errors.New("S3 недоступен"))
	if calls != 2 {
		t.Fatal("неотправленное оповещение было сочтено доставленным")
	}
}
