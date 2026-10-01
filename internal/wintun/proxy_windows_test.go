package wintun

import (
	"strings"
	"testing"
)

func TestProxySettingAloneDoesNotClaimATunnelLeak(t *testing.T) {
	for _, proxy := range []Proxy{
		{Server: "http://127.0.0.1:12334", Owner: "Hiddify"},
		{Server: "proxy.example:8080"},
		{AutoConfig: "https://example.test/proxy.pac"},
	} {
		text := proxy.Describe()
		if text == "" || strings.Contains(text, "мимо туннеля") {
			t.Fatalf("наличие прокси выдано за доказанную утечку: %q", text)
		}
		if proxy.Server != "" && !strings.Contains(text, proxy.Server) {
			t.Fatal("из предупреждения потерян адрес прокси")
		}
		if proxy.Owner != "" && !strings.Contains(text, proxy.Owner) {
			t.Fatal("из предупреждения потеряно имя программы")
		}
	}
	if text := (Proxy{}).Describe(); text != "" {
		t.Fatal("предупреждение показано без прокси")
	}
}
