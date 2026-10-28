//go:build windows

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

func TestReportCarriesTheStoryButNeitherKeysNorAddresses(t *testing.T) {
	link := "marvia://AAAASECRETKEYAAAA@panel.example/sub/tok3nSECRET"
	c := &Controller{
		state:       StateConnected,
		account:     link,
		node:        client.Node{Name: "fi-1", Address: "89.125.41.165:443"},
		log:         newJournal(),
		proxyAt:     time.Now(),
		conflicts:   []string{"conflictHiddify"},
		conflictsAt: time.Now(),
	}
	c.log.add("подключаюсь по %s к 89.125.41.165:443", link)
	c.log.add("нода fi-1 выбрана")

	r := c.Report()
	for _, secret := range []string{"SECRETKEY", "tok3nSECRET", "89.125.41.165"} {
		if strings.Contains(r, secret) {
			t.Fatalf("в отчёте осталось %q:\n%s", secret, r)
		}
	}
	for _, need := range []string{version, "fi-1", "Hiddify", "нода fi-1 выбрана"} {
		if !strings.Contains(r, need) {
			t.Fatalf("в отчёте нет %q:\n%s", need, r)
		}
	}
}
