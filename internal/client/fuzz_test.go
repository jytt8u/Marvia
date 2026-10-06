package client

import "testing"

// Ссылку доступа вставляют из чата и открывают по нажатию в браузере — её
// может прислать кто угодно. Обещание: разбор не падает, а принятая ссылка
// ведёт к подписке только по https.
func FuzzParseAccountLink(f *testing.F) {
	f.Add("marvia://k3j9x2mq8w1z4v7pQwErTy@panel.example.com/sub/AbCdEf0123456789#Мой доступ")
	f.Add("marvia://key@panel.example.com/sub/tok?ip=198.51.100.1,2001:db8::1")
	f.Add("veil-account://key@[::1]:8443/sub/x")
	f.Add("marvia://@/sub/")
	f.Add("")
	f.Fuzz(func(t *testing.T, raw string) {
		acc, err := ParseAccountLink(raw)
		if err != nil {
			return
		}
		if len(acc.SubscriptionURL) < 8 || acc.SubscriptionURL[:8] != "https://" {
			t.Fatalf("подписка не по https: %q", acc.SubscriptionURL)
		}
	})
}
