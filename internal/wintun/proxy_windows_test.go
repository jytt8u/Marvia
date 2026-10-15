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

func TestALocalProxySaysBrowsersGoThroughThatProgramAndWhatToDo(t *testing.T) {
	// Самый частый случай: Hiddify или другой клиент оставил свой прокси.
	// Человек видит «нет интернета» в браузере при работающем туннеле, и
	// фраза «маршрут не проверен» ничего ему не объясняет.
	text := Proxy{Server: "http://127.0.0.1:12334", Owner: "Hiddify"}.Describe()
	for _, want := range []string{"Браузеры", "Hiddify", "127.0.0.1:12334", "Убрать прокси"} {
		if !strings.Contains(text, want) {
			t.Fatalf("в предупреждении нет %q: %q", want, text)
		}
	}
	// Порт никто не слушает — программу закрыли, а настройка осталась.
	dead := Proxy{Server: "127.0.0.1:12334"}.Describe()
	if !strings.Contains(dead, "никто не отвечает") || !strings.Contains(dead, "не откроются") {
		t.Fatalf("мёртвый прокси не назван причиной: %q", dead)
	}
}
