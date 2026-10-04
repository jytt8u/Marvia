package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Настройки переживают перезапуск, а испорченный файл даёт умолчания, а не
// отказ подключаться.
func TestSettingsSurviveRestartAndBrokenFileGivesDefaults(t *testing.T) {
	dir := t.TempDir()
	if got := loadSettings(dir); got != (pcSettings{}) {
		t.Fatalf("без файла: %+v", got)
	}
	want := pcSettings{BypassRussian: true}
	if err := saveSettings(dir, want); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(dir); got != want {
		t.Errorf("после перезапуска: %+v", got)
	}
	if err := os.WriteFile(settingsFile(dir), []byte("{сломано"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadSettings(dir); got != (pcSettings{}) {
		t.Errorf("испорченный файл: %+v", got)
	}
}

// Российский список хранится целиком, а через неделю считается устаревшим.
func TestRussianListIsKeptWholeAndAgesInAWeek(t *testing.T) {
	dir := t.TempDir()
	if list, at := loadRuRoutes(dir); list != nil || !at.IsZero() {
		t.Fatalf("без файла: %v %v", list, at)
	}
	if err := saveRuRoutes(dir, []string{"77.88.0.0/18", "87.240.128.0/18"}); err != nil {
		t.Fatal(err)
	}
	list, at := loadRuRoutes(dir)
	if len(list) != 2 || list[1] != "87.240.128.0/18" {
		t.Errorf("прочитано %v", list)
	}
	if ruRoutesStale(at, at.Add(6*24*time.Hour)) {
		t.Error("шестидневный список уже устарел")
	}
	if !ruRoutesStale(at, at.Add(7*24*time.Hour)) {
		t.Error("недельный список ещё свеж")
	}
	if _, err := os.Stat(filepath.Join(dir, ruRoutesFile+".tmp")); !os.IsNotExist(err) {
		t.Error("временный файл остался")
	}
}
