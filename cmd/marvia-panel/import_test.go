package main

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
)

// oldXUI — база 3x-ui с одним покупателем на одном входе.
func oldXUI(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "x-ui.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, s := range []string{
		`CREATE TABLE inbounds (id INTEGER PRIMARY KEY, enable NUMERIC, port INTEGER, protocol TEXT, settings TEXT, stream_settings TEXT, tag TEXT)`,
		`INSERT INTO inbounds VALUES (1, 1, 443, 'vless',
			'{"clients":[{"id":"6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5b","email":"anna","subId":"s1","enable":true}]}',
			'{"network":"ws","security":"tls"}', 'in-443')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestImportWithoutApplyWritesNothing(t *testing.T) {
	panelDB := filepath.Join(t.TempDir(), "panel.db")
	var out bytes.Buffer
	if err := runImport(&out, importOptions{source: "3x-ui", from: oldXUI(t)}, panelDB); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(panelDB); !os.IsNotExist(err) {
		t.Fatalf("проверка без -import-apply тронула базу панели: %v", err)
	}
	if !strings.Contains(out.String(), "ничего не записано") {
		t.Errorf("отчёт не говорит, что это только проверка:\n%s", out.String())
	}
}

func TestImportApplyMovesCustomersOnceAndOpensOldSubPath(t *testing.T) {
	from := oldXUI(t)
	panelDB := filepath.Join(t.TempDir(), "panel.db")
	opts := importOptions{source: "3x-ui", from: from, apply: true, subPath: "/random7/"}

	for run := 0; run < 2; run++ {
		var out bytes.Buffer
		if err := runImport(&out, opts, panelDB); err != nil {
			t.Fatalf("запуск %d: %v", run+1, err)
		}
	}

	store, err := panel.Open(panelDB)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	list, err := store.ListUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ExternalID != "3x-ui:s1" {
		t.Fatalf("после двух запусков покупатели: %+v", list)
	}
	if !store.IsLegacySubPath(context.Background(), "random7") {
		t.Error("путь подписки прежней панели не открыт")
	}
}
