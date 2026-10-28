package foreign

import "testing"

// TestRouteTellsHowTheForeignNodeIsReached: окно показывает то, с чем движок
// действительно дозванивается, — протокол, транспорт, защиту, имя и
// отпечаток из ссылки, — а не «TLS» по умолчанию.
func TestRouteTellsHowTheForeignNodeIsReached(t *testing.T) {
	cases := []struct {
		link string
		want Route
	}{
		{
			"vless://b831381d-6324-4d53-ad4f-8cda48b30811@nl.example.com:443?type=tcp&security=reality&sni=www.microsoft.com&fp=firefox&pbk=Z84J2IelR9ch3k8VtlVhhs5ycBUlXA7wHBWcBrjqnAw&sid=6ba85179e30d4fc2#nl",
			Route{Protocol: "vless", Network: "raw", Security: "reality", SNI: "www.microsoft.com", Fingerprint: "firefox"},
		},
		{
			"trojan://secret@de.example.com:443?type=ws&path=%2Fws&sni=cdn.example.com#de",
			Route{Protocol: "trojan", Network: "ws", Security: "tls", SNI: "cdn.example.com", Fingerprint: "chrome"},
		},
	}
	for _, c := range cases {
		l, err := Parse(c.link)
		if err != nil {
			t.Fatalf("%s: %v", c.link, err)
		}
		if got := l.Route(); got != c.want {
			t.Errorf("%s:\n  есть   %+v\n  нужно  %+v", c.link, got, c.want)
		}
	}
}
