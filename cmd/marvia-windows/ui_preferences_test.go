//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUISettingsSurviveNewWindowOrigin(t *testing.T) {
	config := t.TempDir()
	t.Setenv("APPDATA", config)

	if got := readUISetting("language"); got != "" {
		t.Fatalf("new profile has language %q", got)
	}
	if err := writeUISetting("language", "en"); err != nil {
		t.Fatal(err)
	}
	if err := writeUISetting("welcome", "1"); err != nil {
		t.Fatal(err)
	}
	if got := readUISetting("language"); got != "en" {
		t.Fatalf("language after restart = %q", got)
	}
	if got := readUISetting("welcome"); got != "1" {
		t.Fatalf("first-run flag after restart = %q", got)
	}
	if _, err := os.Stat(filepath.Join(config, "Marvia", "ui-language")); err != nil {
		t.Fatalf("language not stored in app config: %v", err)
	}
}
