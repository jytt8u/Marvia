package httpguard

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func request(address string) *http.Request {
	u, err := url.Parse(address)
	if err != nil {
		panic(err)
	}
	return &http.Request{URL: u}
}

func TestEveryRedirectKeepsHTTPSOnceItWasUsed(t *testing.T) {
	client := SubscriptionClient(http.DefaultClient)
	for _, test := range []struct {
		name        string
		destination string
		previous    []string
		blocked     bool
	}{
		{"защищённый переход", "https://other.example/sub/token", []string{"https://panel.example/sub/token"}, false},
		{"старый HTTP", "http://other.example/sub/token", []string{"http://panel.example/sub/token"}, false},
		{"переход на TLS", "https://panel.example/sub/token", []string{"http://panel.example/sub/token"}, false},
		{"потеря TLS", "http://panel.example/sub/token", []string{"https://panel.example/sub/token"}, true},
		{"потеря TLS после старого HTTP", "http://panel.example/sub/token", []string{"http://panel.example/sub/token", "https://panel.example/sub/token"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var via []*http.Request
			for _, address := range test.previous {
				via = append(via, request(address))
			}
			if err := client.CheckRedirect(request(test.destination), via); (err != nil) != test.blocked {
				t.Fatalf("неверная политика перенаправления: %v", err)
			}
		})
	}
}

func TestGuardPreservesExistingClientAndRedirectLimits(t *testing.T) {
	cause := errors.New("перенаправления запрещены владельцем клиента")
	base := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return cause }}
	guarded := SubscriptionClient(base)
	if !errors.Is(guarded.CheckRedirect(request("https://panel.example"), nil), cause) {
		t.Fatal("политика исходного клиента потеряна")
	}
	if guarded == base {
		t.Fatal("изменён исходный клиент")
	}
	via := make([]*http.Request, 10)
	for i := range via {
		via[i] = request("https://panel.example")
	}
	if SubscriptionClient(http.DefaultClient).CheckRedirect(request("https://panel.example"), via) == nil {
		t.Fatal("потерян предел перенаправлений")
	}
}
