//go:build windows

package main

import (
	"encoding/json"
	"github.com/jytt8u/marvia/internal/client"
	"testing"
)

// Окно получает версию и ссылку из Status; по непустому update показывается
// карточка. Проверяем границу подписки и данных окна без создания VPN-адаптера.
func TestWindowsWindowReceivesTheNewerTestReleaseAndItsDownloadLink(t *testing.T) {
	const url = "https://panel.example.test/sub/test/app/windows"
	sub := client.Subscription{Apps: map[string]client.AppOffer{"windows": {Version: "0.13.0-alpha.6", URL: url}}}
	for _, current := range []string{"0.13.0-alpha.5", "0.13.0-alpha.6", "0.13.0-beta.1", "dev"} {
		offer, _ := sub.Update("windows", current)
		c := &Controller{state: StateConnected, known: sub, update: offer}
		raw, err := json.Marshal(c.Status())
		if err != nil {
			t.Fatal(err)
		}
		var view struct {
			Update string `json:"update"`
			URL    string `json:"update_url"`
		}
		if err := json.Unmarshal(raw, &view); err != nil {
			t.Fatal(err)
		}
		if current == "0.13.0-alpha.5" {
			if view.Update != "0.13.0-alpha.6" || view.URL != url {
				t.Fatalf("окно не получило предложение: %+v", view)
			}
		} else if view.Update != "" || view.URL != "" {
			t.Fatalf("%s: окну предложен откат или повтор: %+v", current, view)
		}
	}
}
