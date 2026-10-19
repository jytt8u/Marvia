package tunbridge_test

import (
	"errors"
	"net/netip"
	"testing"

	"github.com/jytt8u/marvia/internal/tunbridge"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Человек вписывает сайт как угодно — из строки браузера, со звёздочкой,
// кириллицей, — а правило получается одно и то же.
func TestSiteIsAcceptedTheWayAPersonTypesIt(t *testing.T) {
	cases := map[string]string{
		"sberbank.ru":                          "sberbank.ru",
		"  Online.Sberbank.RU ":                "online.sberbank.ru",
		"https://online.sberbank.ru/CSAFront/": "online.sberbank.ru",
		"*.gosuslugi.ru":                       "gosuslugi.ru",
		".gosuslugi.ru.":                       "gosuslugi.ru",
		"http://user@host.example:8443/?q=1":   "host.example",
		"госуслуги.рф":                         "xn--c1aapkosapc.xn--p1ai",
		"рф":                                   "xn--p1ai",
	}
	for in, want := range cases {
		got, err := tunbridge.NormalizeSite(in)
		if err != nil || got != want {
			t.Errorf("%q → %q, %v; ждали %q", in, got, err, want)
		}
	}
}

// Не имя — отказ, а не правило, которое молча ничего не делает.
func TestNotASiteIsRefused(t *testing.T) {
	for _, in := range []string{"", "   ", "*.", "сбер банк", "1.2.3.4", "[2a00::1]", "a_b.ru", "-bad.ru"} {
		if got, err := tunbridge.NormalizeSite(in); !errors.Is(err, tunbridge.ErrBadSite) {
			t.Errorf("%q принят как %q", in, got)
		}
	}
	_, err := tunbridge.NormalizeSites([]string{"ya.ru", "сбер банк"})
	var bad *tunbridge.BadSiteError
	if !errors.As(err, &bad) || bad.Site != "сбер банк" {
		t.Fatalf("ошибка не называет неверную строку: %v", err)
	}
}

// Повторы в списке схлопываются, порядок человека сохраняется.
func TestSiteListKeepsOrderAndDropsRepeats(t *testing.T) {
	got, err := tunbridge.NormalizeSites([]string{"vk.com", "Ya.ru", "*.vk.com", "ya.ru"})
	if err != nil || len(got) != 2 || got[0] != "vk.com" || got[1] != "ya.ru" {
		t.Fatalf("%v, %v", got, err)
	}
}

// Самое точное правило сильнее общего: «мимо» на всю зону .ru, но vk.ru —
// через туннель.
func TestMostSpecificSiteRuleWins(t *testing.T) {
	s := tunbridge.NewSites()
	s.Set([]string{"ru"}, []string{"vk.ru"})
	ip := netip.MustParseAddr("198.51.100.1")
	if got := s.Route("mail.ru", ip); got != tunbridge.RouteDirect {
		t.Errorf("mail.ru: %v", got)
	}
	if got := s.Route("m.vk.ru", ip); got != tunbridge.RouteTunnel {
		t.Errorf("m.vk.ru: %v", got)
	}
	if got := s.Route("example.com", ip); got != tunbridge.RouteAny {
		t.Errorf("example.com: %v", got)
	}
}

// Сайт из списка «через туннель» остаётся в туннеле, даже если его адрес в
// российском списке; остальные российские адреса по-прежнему идут мимо.
func TestTunnelRuleKeepsASiteInTheTunnelDespiteTheRussianList(t *testing.T) {
	s := tunbridge.NewSites()
	s.Set(nil, []string{"vk.com"})
	var tunnel, local recordDialer
	d := tunbridge.Split(&tunnel, &local, func(netip.Addr) bool { return true }, s)

	_, _ = d.DialTarget(tunbridge.WithSiteForTest("vk.com"), addr("87.240.132.72", 443))
	_, _ = d.DialTarget(tunbridge.WithSiteForTest("ya.ru"), addr("77.88.55.242", 443))
	if len(tunnel.got) != 1 || tunnel.got[0] != "tcp 87.240.132.72:443" {
		t.Errorf("в туннель: %v", tunnel.got)
	}
	if len(local.got) != 1 || local.got[0] != "tcp 77.88.55.242:443" {
		t.Errorf("мимо: %v", local.got)
	}
}

// Правило, добавленное при поднятом туннеле, действует на новые соединения
// сразу, без переподключения.
func TestSiteRulesChangeWhileTheTunnelIsUp(t *testing.T) {
	s := tunbridge.NewSites()
	var tunnel, local recordDialer
	d := tunbridge.Split(&tunnel, &local, nil, s)
	ctx := tunbridge.WithSiteForTest("sberbank.ru")
	_, _ = d.DialTarget(ctx, addr("203.0.113.7", 443))
	s.Set([]string{"sberbank.ru"}, nil)
	_, _ = d.DialTarget(ctx, addr("203.0.113.7", 443))
	if len(tunnel.got) != 1 || len(local.got) != 1 {
		t.Errorf("в туннель: %v, мимо: %v", tunnel.got, local.got)
	}
}

// Общий адрес CDN у сайта из обоих списков ведёт в туннель: ошибиться в
// сторону туннеля безопаснее.
func TestSharedAddressGoesThroughTheTunnelWhenRulesDisagree(t *testing.T) {
	s := tunbridge.NewSites()
	s.Set([]string{"bank.example"}, []string{"news.example"})
	ip := netip.MustParseAddr("198.51.100.77")
	tunbridge.LearnForTest(s, "bank.example", ip)
	if got := s.Route("", ip); got != tunbridge.RouteDirect {
		t.Fatalf("один сайт на адресе: %v", got)
	}
	tunbridge.LearnForTest(s, "news.example", ip)
	if got := s.Route("", ip); got != tunbridge.RouteTunnel {
		t.Fatalf("два сайта на адресе: %v", got)
	}
}

func addr(host string, port uint16) vp1.Address {
	return vp1.Address{Type: vp1.AtypIPv4, Host: host, Port: port}
}

// Окно показывает кириллическое имя кириллицей, а присланное обратно оно
// даёт то же правило.
func TestCyrillicSiteIsShownAsTypedAndComesBackTheSame(t *testing.T) {
	rule, err := tunbridge.NormalizeSite("госуслуги.рф")
	if err != nil {
		t.Fatal(err)
	}
	shown := tunbridge.DisplaySite(rule)
	if shown != "госуслуги.рф" {
		t.Fatalf("окно показывает %q", shown)
	}
	if back, err := tunbridge.NormalizeSite(shown); err != nil || back != rule {
		t.Fatalf("обратно %q, %v", back, err)
	}
}
