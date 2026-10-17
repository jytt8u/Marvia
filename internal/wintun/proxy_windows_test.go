package wintun

import (
	"net"
	"os"
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

func TestAProxyListeningOnlyOnIPv6LoopbackIsFound(t *testing.T) {
	// Прокси на одном [::1] — живой; не найти его владельца значило бы
	// написать человеку «никто не отвечает», хотя прокси работает.
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 loopback недоступен: %v", err)
	}
	defer ln.Close()
	port := uint16(ln.Addr().(*net.TCPAddr).Port)
	pid, ok := listenerPID(port)
	if !ok || pid != uint32(os.Getpid()) {
		t.Fatalf("слушатель [::1]:%d не найден: pid=%d ok=%v", port, pid, ok)
	}
}

func TestOnlyAnAbandonedLocalProxyCountsAsDead(t *testing.T) {
	cases := []struct {
		p    Proxy
		dead bool
	}{
		{Proxy{Server: "127.0.0.1:12334"}, true},
		{Proxy{Server: "http://127.0.0.1:12334", Listening: true, Owner: "Hiddify.exe"}, false},
		// Корпоративный прокси на другой машине мы не проверяем и не трогаем.
		{Proxy{Server: "proxy.corp.local:3128"}, false},
		// Переменные окружения из реестра не снять — это не «мусор в реестре».
		{Proxy{Server: "127.0.0.1:12334", FromEnv: true}, false},
		{Proxy{AutoConfig: "http://wpad/wpad.dat"}, false},
		{Proxy{}, false},
	}
	for _, c := range cases {
		if got := c.p.Dead(); got != c.dead {
			t.Errorf("%+v: мёртвый=%v, ожидалось %v", c.p, got, c.dead)
		}
	}
}
