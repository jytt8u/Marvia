package panel

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAlertsForgetRemovedOrDisabledNodes(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatalf("база: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.SetAlertSettings(ctx, AlertSettings{Enabled: true, BotToken: "test", ChatID: "1"}); err != nil {
		t.Fatalf("настройки оповещений: %v", err)
	}

	removed, _, err := store.CreateNode(ctx, CreateNodeParams{Name: "удалённая", Address: "198.51.100.1:443"})
	if err != nil {
		t.Fatalf("удалённая нода: %v", err)
	}
	disabled, _, err := store.CreateNode(ctx, CreateNodeParams{Name: "выключенная", Address: "198.51.100.2:443"})
	if err != nil {
		t.Fatalf("выключаемая нода: %v", err)
	}

	alerts := NewAlerts(store)
	alerts.SendWith(func(context.Context, AlertSettings, string) error { return nil })
	alerts.Check(ctx)
	if got := len(alerts.told); got != 2 {
		t.Fatalf("состояний молчащих нод %d, ожидалось 2", got)
	}

	if err := store.DeleteNode(ctx, removed.ID); err != nil {
		t.Fatalf("удаление ноды: %v", err)
	}
	off := false
	if _, err := store.UpdateNode(ctx, disabled.ID, UpdateNodeParams{Enabled: &off}); err != nil {
		t.Fatalf("выключение ноды: %v", err)
	}
	alerts.Check(ctx)

	if got := len(alerts.told); got != 0 {
		t.Fatalf("после удаления и выключения нод осталось %d записей оповещений", got)
	}
}
