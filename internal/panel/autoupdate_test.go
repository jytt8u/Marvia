package panel_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/updater"
)

// TestAutoUpdateAsksOnlyInItsHourAndOnlyOnce: включённое автообновление в
// свой час по Москве просит отставшие ноды и службу панели; в другой час и
// выключенное — не трогает ничего, а один и тот же выпуск за ночь дважды не
// просит: служба на каждую просьбу качает приложения заново.
func TestAutoUpdateAsksOnlyInItsHourAndOnlyOnce(t *testing.T) {
	home := t.TempDir()
	store, err := panel.Open(filepath.Join(t.TempDir(), "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	api := panel.NewAPI(store, adminToken, "https://sub.example.com", t.TempDir()).WithHome(home).WithVersion("v0.11.0")
	server := httptest.NewServer(api.Handler())
	t.Cleanup(server.Close)
	h := &harness{t: t, server: server}
	stale := h.createNode("stale")
	h.do(http.MethodPost, "/api/v1/node/usage", stale.Token, map[string]any{"usage": map[string]any{}, "version": "v0.11.0"}, nil)
	defer h.fakeRelease("v0.12.0")()
	was := updater.AgentPath
	updater.AgentPath = filepath.Join(t.TempDir(), "agent.sh")
	t.Cleanup(func() { updater.AgentPath = was })
	_ = os.WriteFile(updater.AgentPath, []byte("#!/bin/sh\n"), 0o755)

	msk := time.FixedZone("MSK", 3*60*60)
	at := func(hour int) time.Time { return time.Date(2026, 10, 21, hour, 15, 0, 0, msk) }
	ctx := context.Background()
	asked := func() bool {
		_, err := os.Stat(updater.RequestPath(home))
		return err == nil
	}

	if err := api.AutoUpdateAt(ctx, at(4)); err != nil || asked() {
		t.Fatalf("выключенное автообновление что-то попросило: %v", err)
	}
	if code := h.do(http.MethodPut, "/api/v1/updates/auto", adminToken, map[string]any{"enabled": true, "hour": 4}, nil); code != http.StatusOK {
		t.Fatalf("включение: код %d", code)
	}
	var cfg panel.AutoUpdateSettings
	h.do(http.MethodGet, "/api/v1/updates/auto", adminToken, nil, &cfg)
	if !cfg.Enabled || cfg.Hour != 4 {
		t.Fatalf("настройка не сохранилась: %+v", cfg)
	}
	if code := h.do(http.MethodPut, "/api/v1/updates/auto", adminToken, map[string]any{"enabled": true, "hour": 24}, nil); code != http.StatusBadRequest {
		t.Fatalf("час 24 принят: код %d", code)
	}

	if err := api.AutoUpdateAt(ctx, at(13)); err != nil || asked() {
		t.Fatalf("автообновление днём: %v", err)
	}
	if err := api.AutoUpdateAt(ctx, at(4)); err != nil || !asked() {
		t.Fatalf("в свой час панель не попросила обновиться: %v", err)
	}
	var out struct {
		UpgradeTo string `json:"upgrade_to"`
	}
	h.do(http.MethodGet, "/api/v1/node/users", stale.Token, nil, &out)
	if out.UpgradeTo != "v0.12.0" {
		t.Fatalf("отставшая нода не получила просьбу: %q", out.UpgradeTo)
	}

	_ = os.Remove(updater.RequestPath(home))
	if err := api.AutoUpdateAt(ctx, at(4).Add(20*time.Minute)); err != nil || asked() {
		t.Fatalf("тот же выпуск попрошен второй раз за ночь: %v", err)
	}
}
