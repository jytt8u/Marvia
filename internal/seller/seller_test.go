package seller

import (
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOnlyHTTPSAndTelegramLinksAreAccepted(t *testing.T) {
	good := []string{
		"https://t.me/seller_support",
		"https://shop.example.com/renew?plan=month",
		"tg://resolve?domain=seller_bot",
		"  https://example.com/  ",
		"",
	}
	for _, link := range good {
		if _, err := CheckLink(link); err != nil {
			t.Errorf("%q отвергнута: %v", link, err)
		}
	}

	bad := []string{
		"http://example.com/renew",
		"javascript:alert(1)",
		"file:///etc/passwd",
		"intent://scan/#Intent;scheme=zxing;end",
		"ftp://example.com",
		"https://",
		"https://bank.example@evil.example/pay",
		"https://example.com/\r\nSet-Cookie: x=1",
		"https://пример.рф/продлить",
		"tg:resolve?domain=x",
		"example.com",
		"https://example.com/" + strings.Repeat("a", MaxLink),
	}
	for _, link := range bad {
		if _, err := CheckLink(link); err == nil {
			t.Errorf("%q принята, хотя не должна", link)
		}
	}
}

func TestAnnouncementLongerThanLimitIsRefusedByPanel(t *testing.T) {
	if _, err := CheckAnnounce(strings.Repeat("я", MaxAnnounce)); err != nil {
		t.Fatalf("ровно %d символов кириллицей отвергнуты: %v", MaxAnnounce, err)
	}
	if _, err := CheckAnnounce(strings.Repeat("я", MaxAnnounce+1)); err == nil {
		t.Fatal("объявление длиннее предела принято")
	}
}

func TestAnnouncementLosesControlAndDirectionCharacters(t *testing.T) {
	got := Clean(Info{Announce: "Работы\nс 2:00\x07 до‮ 4:00⁦‏ \t"}).Announce
	if got != "Работы с 2:00 до 4:00" {
		t.Fatalf("после чистки %q", got)
	}
}

func TestClientDropsBadLinksAndTrimsLongAnnouncement(t *testing.T) {
	got := Clean(Info{
		SupportURL: "javascript:alert(1)",
		RenewURL:   "https://shop.example.com/renew",
		Announce:   strings.Repeat("ж", MaxAnnounce+50),
	})
	if got.SupportURL != "" {
		t.Errorf("опасная ссылка поддержки осталась: %q", got.SupportURL)
	}
	if got.RenewURL != "https://shop.example.com/renew" {
		t.Errorf("годная ссылка продления потерялась: %q", got.RenewURL)
	}
	if n := utf8.RuneCountInString(got.Announce); n > MaxAnnounce {
		t.Errorf("объявление в %d символов не обрезано", n)
	}
}

func TestHeadersSurviveTheRoundTripWithCyrillic(t *testing.T) {
	in := Info{
		SupportURL: "tg://resolve?domain=seller_support",
		RenewURL:   "https://t.me/seller_bot",
		Announce:   "Профилактика ночью 🛠",
	}
	h := http.Header{}
	in.WriteHeader(h, "Мой VPN")

	for _, name := range []string{HeaderAnnounce, HeaderTitle} {
		if v := h.Get(name); !strings.HasPrefix(v, "base64:") {
			t.Errorf("%s без приставки base64: %q", name, v)
		}
	}
	for name, values := range h {
		for _, v := range values {
			for i := 0; i < len(v); i++ {
				if v[i] >= 0x7f {
					t.Errorf("в заголовке %s не латиница: %q", name, v)
					break
				}
			}
		}
	}
	if got := FromHeader(h); got != in {
		t.Fatalf("прочитали %+v, писали %+v", got, in)
	}
}

func TestEmptyFieldsWriteNoHeaders(t *testing.T) {
	h := http.Header{}
	Info{}.WriteHeader(h, "")
	if len(h) != 0 {
		t.Fatalf("на пустых настройках ушли заголовки: %v", h)
	}
}

func TestForeignAnnouncementIsReadInEitherForm(t *testing.T) {
	cases := map[string]string{
		"Плановые работы":                                 "Плановые работы",
		"base64:0J/Qu9Cw0L3QvtCy0YvQtSDRgNCw0LHQvtGC0Ys=": "Плановые работы",
		"BASE64:0J/Qu9Cw0L3QvtCy0YvQtSDRgNCw0LHQvtGC0Ys":  "Плановые работы",
		"base64:это-не-base64!":                           "",
	}
	for raw, want := range cases {
		h := http.Header{}
		h.Set(HeaderAnnounce, raw)
		if got := FromHeader(h).Announce; got != want {
			t.Errorf("%q прочитано как %q, ожидалось %q", raw, got, want)
		}
	}
}
