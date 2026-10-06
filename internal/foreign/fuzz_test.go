package foreign

import (
	"encoding/json"
	"testing"
)

// Ссылки и подписки приходят от чужих панелей и из буфера обмена — то есть
// от кого угодно. Обещание — разбор не падает ни на каких байтах, а то, что
// он принял, превращается в конфиг движка без ошибки.

func FuzzParseLink(f *testing.F) {
	for _, seed := range []string{
		"vless://6f1a6c2e-3d4b-4e5f-8a9b-0c1d2e3f4a5b@198.51.100.7:443?security=reality&pbk=abc&sid=01&sni=www.microsoft.com&type=tcp#Финляндия",
		"trojan://pass@example.com:443?sni=example.com#x",
		"vmess://eyJhZGQiOiJleGFtcGxlLmNvbSIsInBvcnQiOiI0NDMiLCJpZCI6IjZmMWE2YzJlLTNkNGItNGU1Zi04YTliLTBjMWQyZTNmNGE1YiJ9",
		"ss://YWVzLTI1Ni1nY206cGFzcw@198.51.100.1:8388#ss",
		"hysteria2://pass@example.com:443?sni=example.com",
		"wireguard://key@198.51.100.2:51820?publickey=abc&address=10.0.0.2/32",
		"vless://@:0", "://", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		l, err := Parse(raw)
		if err != nil {
			return
		}
		if l.Port < 0 || l.Port > 65535 {
			t.Fatalf("порт вне диапазона: %d", l.Port)
		}
		if _, err := json.Marshal(l.Outbound); err != nil {
			t.Fatalf("принятая ссылка не превращается в конфиг: %v", err)
		}
	})
}

func FuzzParseList(f *testing.F) {
	f.Add([]byte("dmxlc3M6Ly82ZjFhNmMyZS0zZDRiLTRlNWYtOGE5Yi0wYzFkMmUzZjRhNWJAMTk4LjUxLjEwMC43OjQ0Mw=="))
	f.Add([]byte("trojan://pass@example.com:443#a\n# комментарий\nvless://x@y:1"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, body []byte) {
		sub := ParseList(body)
		if sub.Skipped < 0 {
			t.Fatal("отрицательный счётчик пропущенных")
		}
	})
}

func FuzzParseUserinfo(f *testing.F) {
	f.Add("upload=1; download=2; total=10737418240; expire=1767225600")
	f.Add("total=-1; expire=99999999999999999999")
	f.Add("")
	f.Fuzz(func(t *testing.T, raw string) {
		var sub Subscription
		sub.ParseUserinfo(raw)
		if sub.Total < 0 || sub.Upload < 0 || sub.Download < 0 {
			t.Fatalf("отрицательный трафик из заголовка: %+v", sub)
		}
	})
}

func TestHostileUserinfoNeverShowsMoreLeftThanTheLimit(t *testing.T) {
	var sub Subscription
	sub.ParseUserinfo("upload=-5000; download=9223372036854775807; total=1000")
	if sub.Upload != 0 || sub.Used() < 0 || sub.Remaining() < 0 || sub.Remaining() > sub.Total {
		t.Fatalf("остаток %d при лимите %d, расход %d", sub.Remaining(), sub.Total, sub.Used())
	}
	sub = Subscription{}
	sub.ParseUserinfo("upload=9223372036854775807; download=9223372036854775807; total=10")
	if sub.Used() < 0 || sub.Remaining() != 0 {
		t.Fatalf("переполнение суммы: расход %d, остаток %d", sub.Used(), sub.Remaining())
	}
}
