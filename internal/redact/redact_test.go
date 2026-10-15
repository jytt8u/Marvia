package redact_test

import (
	"bytes"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/redact"
)

// TestAddressNeverSurvivesTheJournal — ни один вид адреса не доходит до журнала.
//
// Строки взяты из того, что реально пишут net/http и ошибки сети: адрес там
// бывает голым, с портом, в квадратных скобках и сразу перед двоеточием фразы.
func TestAddressNeverSurvivesTheJournal(t *testing.T) {
	cases := map[string]string{
		"рукопожатие TLS":   "http: TLS handshake error from 203.0.113.7:51234: EOF",
		"IPv6 со скобками":  "http: TLS handshake error from [2001:db8::7]:51234: EOF",
		"IPv6 голый":        "dial udp 2001:db8::7: network is unreachable",
		"оба конца":         "write tcp 192.0.2.1:443->203.0.113.7:51234: write: broken pipe",
		"конец предложения": "пришли с 203.0.113.7.",
		"паника":            "http: panic serving 203.0.113.7:51234: runtime error",
	}
	for name, line := range cases {
		got := redact.Addresses(line)
		for _, leak := range []string{"203.0.113.7", "2001:db8::7", "51234"} {
			if strings.Contains(got, leak) {
				t.Errorf("%s: в журнал уехало %q: %q", name, leak, got)
			}
		}
		if !strings.Contains(got, redact.Placeholder) {
			t.Errorf("%s: адрес пропал без пометки, оператор не поймёт, что тут было: %q", name, got)
		}
	}
}

// TestRedactionKeepsTheRestOfTheLine — время, версии и причины остаются.
//
// Пометка вместо всего подряд, похожего на цифры, сделала бы журнал
// бесполезным: «ошибка в <адрес>» на месте времени ничего не объясняет.
func TestRedactionKeepsTheRestOfTheLine(t *testing.T) {
	for _, line := range []string{
		"копия базы снята в 12:30:45",
		"marvia-node v0.13.0 слушает :443",
		"сессия vp1 закрыта",
		"ключ deadbeef.cafe не подошёл",
	} {
		if got := redact.Addresses(line); got != line {
			t.Errorf("строка без адресов изменилась: %q → %q", line, got)
		}
	}
	if got := redact.Addresses("from 203.0.113.7:51234: EOF"); got != "from "+redact.Placeholder+": EOF" {
		t.Errorf("вокруг адреса потерялся смысл: %q", got)
	}
}

// TestLoggerWritesToTheProcessJournalWithoutAddresses — журнал для
// http.Server пишет туда же, куда и остальная программа, но уже без адресов.
func TestLoggerWritesToTheProcessJournalWithoutAddresses(t *testing.T) {
	var buf bytes.Buffer
	was, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(was); log.SetFlags(flags) })

	redact.Logger().Printf("http: TLS handshake error from %s: %v", "203.0.113.7:51234", "EOF")

	got := buf.String()
	if strings.Contains(got, "203.0.113.7") {
		t.Fatalf("адрес дошёл до журнала: %q", got)
	}
	if !strings.Contains(got, "TLS handshake error") {
		t.Fatalf("строка не дошла до журнала вовсе: %q", got)
	}
}

// TestAccessSecretsNeverSurviveAnError — ссылка может прийти из стандартной
// библиотеки, Xray или HTTP-сервера целиком. Тот, кто показывает ошибку или
// пишет её в журнал, не должен угадывать формат каждой из них заново.
func TestAccessSecretsNeverSurviveAnError(t *testing.T) {
	cases := []struct {
		name    string
		line    string
		secrets []string
	}{
		{
			name:    "подписка Marvia",
			line:    `Get "https://panel.example/sub/subscription-secret?format=json": connection refused`,
			secrets: []string{"subscription-secret"},
		},
		{
			name:    "прежняя HTTPS-подписка",
			line:    `Get "https://panel.example/legacy-prefix/legacy-subscription-secret": connection refused`,
			secrets: []string{"legacy-subscription-secret"},
		},
		{
			name:    "ключ Marvia",
			line:    `parse "marvia://private-key-secret@panel.example/sub/subscription-secret": invalid port`,
			secrets: []string{"private-key-secret", "subscription-secret"},
		},
		{
			name:    "VLESS",
			line:    `не подошла vless://uuid-secret@node.example:443?security=tls`,
			secrets: []string{"uuid-secret"},
		},
		{
			name:    "Trojan",
			line:    `не подошла trojan://password-secret@node.example:443`,
			secrets: []string{"password-secret"},
		},
		{
			name:    "пароль со знаком препинания",
			line:    `не подошла trojan://password-secret!`,
			secrets: []string{"password-secret", "!"},
		},
		{
			name:    "ссылка без пробела перед схемой",
			line:    `адресtrojan://joined-password-secret@node.example:443`,
			secrets: []string{"joined-password-secret"},
		},
		{
			name:    "Shadowsocks",
			line:    `не подошла ss://method:password-secret@node.example:443`,
			secrets: []string{"password-secret"},
		},
		{
			name:    "VMess",
			line:    `не подошла vmess://opaque-uuid-secret`,
			secrets: []string{"opaque-uuid-secret"},
		},
		{
			name:    "Hysteria2",
			line:    `не подошла hysteria2://auth-secret@node.example:443?obfs-password=mask-secret`,
			secrets: []string{"auth-secret", "mask-secret"},
		},
		{
			name:    "WireGuard",
			line:    `не подошла wireguard://private-secret@node.example:51820?presharedkey=shared-secret`,
			secrets: []string{"private-secret", "shared-secret"},
		},
		{
			name:    "путь запроса подписки",
			line:    `GET /sub/subscription-secret/app/android HTTP/1.1`,
			secrets: []string{"subscription-secret"},
		},
		{
			name:    "прежний путь подписки",
			line:    `GET /legacy-prefix/subscription-secret HTTP/1.1`,
			secrets: []string{"subscription-secret"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redact.Error(errors.New(tc.line))
			for _, secret := range tc.secrets {
				if strings.Contains(got, secret) {
					t.Errorf("секрет %q остался в ошибке: %q", secret, got)
				}
			}
		})
	}
}

func TestAWebSocketAddressIsNotMistakenForAShadowsocksLink(t *testing.T) {
	// «wss://» содержит «ss://». Принять его за ссылку Shadowsocks значило бы
	// стереть из журнала имя узла WebSocket — ровно то, по чему ищут, куда
	// не дошло подключение за CDN.
	got := redact.Secrets("рукопожатие wss://cdn.example.net/assets/app.js не прошло")
	if !strings.Contains(got, "cdn.example.net") {
		t.Fatalf("адрес WebSocket пропал: %q", got)
	}
	if got := redact.Secrets("ключ ss://YWVzOnBhc3M@host:8388 не подошёл"); strings.Contains(got, "YWVzOnBhc3M") {
		t.Fatalf("пароль Shadowsocks остался: %q", got)
	}
}
