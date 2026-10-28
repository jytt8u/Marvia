//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Кэш прежней версии переезжает к своему ключу и становится виден окну —
// кнопки продавца есть сразу после обновления, до первого подключения. Кэш
// другого продавца под чужое имя не переезжает.
func TestLegacyCacheMovesToItsOwnKeyOnly(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	dir, err := settingsDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	pair, _ := vp1.GenerateKeyPair()
	link := "marvia://" + vp1.EncodeKey(pair.Private) + "@panel.example.com/sub/abc123"
	account, err := client.ParseAccountLink(link)
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(dir, "subscription.json")
	sub := client.Subscription{
		Nodes:    []client.Node{{ID: 1, Name: "nl", Address: "203.0.113.1:443"}},
		RenewURL: "https://shop.example/renew",
	}

	if err := client.SaveCache(legacy, "https://other.example/sub/zzz", sub); err != nil {
		t.Fatal(err)
	}
	adoptLegacyCache(link)
	if _, err := os.Stat(legacy); err != nil {
		t.Fatal("кэш чужого продавца переехал к этому ключу")
	}

	if err := client.SaveCache(legacy, account.SubscriptionURL, sub); err != nil {
		t.Fatal(err)
	}
	adoptLegacyCache(link)
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("кэш прежней версии остался на месте")
	}
	if got := knownSubscription(link); got.RenewURL != sub.RenewURL {
		t.Fatalf("окно не видит переехавший кэш: %+v", got)
	}
}
