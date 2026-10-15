package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
)

// TestNodePanelErrorDoesNotShowSubscriptionToken — адрес панели печатается
// при ошибке запуска ноды, поэтому случайно переданная ссылка подписки не
// должна превращаться в строку журнала systemd.
func TestNodePanelErrorDoesNotShowSubscriptionToken(t *testing.T) {
	const token = "node-subscription-secret"
	_, _, _, err := preparePanelUsers(context.Background(), serverOptions{
		panelURL:   "https://panel.example/sub/" + token + "%zz",
		panelToken: "node-token-long-enough",
		usageFile:  t.TempDir() + "/usage.json",
	})
	if err == nil {
		t.Fatal("кривая ссылка панели принята")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("токен подписки попал в ошибку запуска ноды: %q", err)
	}
}

// TestNodeSyncJournalDoesNotShowSubscriptionToken — повторная синхронизация
// пишет причину в journald и не должна раскрывать адрес подписки из неё.
func TestNodeSyncJournalDoesNotShowSubscriptionToken(t *testing.T) {
	const token = "node-sync-subscription-secret"
	var buf bytes.Buffer
	was, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(was); log.SetFlags(flags) })

	logPanelSyncError(errors.New("Get https://panel.example/sub/" + token + ": connection refused"))
	if got := buf.String(); strings.Contains(got, token) {
		t.Fatalf("токен подписки остался в журнале синхронизации ноды: %q", got)
	}
}
