package seller

import (
	"net/http"
	"strings"
	"testing"
	"unicode"
)

// Ссылки и объявление продавца приходят из заголовков подписки — своей или
// чужой панели — и попадают на экран и в систему по нажатию. Обещание:
// наружу выходят только https:// и tg://, а в объявлении нет управляющих
// символов и символов направления письма.
func FuzzSellerHeaders(f *testing.F) {
	f.Add("https://t.me/support", "tg://resolve?domain=bot", "base64:0J/RgNC40LLQtdGC")
	f.Add("javascript:alert(1)", "intent://x#Intent;end", "\u202eтекст\u0007")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, support, renew, announce string) {
		h := http.Header{}
		h.Set("Support-Url", support)
		h.Set("Profile-Web-Page-Url", renew)
		h.Set("Announce", announce)
		info := FromHeader(h)
		for _, link := range []string{info.SupportURL, info.RenewURL} {
			if link != "" && !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "tg://") {
				t.Fatalf("пропущена ссылка со схемой не https и не tg: %q", link)
			}
		}
		for _, r := range info.Announce {
			if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
				t.Fatalf("в объявлении остался символ %U", r)
			}
		}
	})
}
