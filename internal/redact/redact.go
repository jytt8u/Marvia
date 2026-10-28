// Package redact вычищает сетевые адреса и секреты из строк, уходящих в
// журнал или человеку на экран.
//
// Зачем отдельный пакет. Свои строки журнала панель и нода пишут аккуратно:
// ни адреса покупателя, ни адреса назначения в них нет. Но часть строк пишем
// не мы. net/http сам сообщает в журнал «http: TLS handshake error from
// 203.0.113.7:51234: EOF», ошибки сети несут оба конца соединения внутри
// себя. Такая строка — это запись «с этого адреса к нам приходили», то есть
// ровно тот журнал, которого у панели и ноды быть не должно (docs/privacy.md).
//
// Выбросить эти строки целиком было бы проще, но тогда продавец не узнает, что
// у него сыплются ошибки TLS или паника в обработчике. Поэтому строка остаётся,
// а адрес в ней заменяется пометкой.
package redact

import (
	"log"
	"net"
	"regexp"
	"strings"
)

// Placeholder — чем заменяется адрес.
const Placeholder = "<адрес>"

// SecretPlaceholder — чем заменяется секретная часть ссылки.
const SecretPlaceholder = "<секрет>"

// candidate — всё, что по набору символов может оказаться адресом: IPv4,
// IPv6 в квадратных скобках или без, с портом и без. Решает не выражение, а
// net.ParseIP: регулярное выражение для IPv6 целиком вышло бы и длинным, и
// неверным, а разбор адреса в стандартной библиотеке уже есть.
var candidate = regexp.MustCompile(`[0-9A-Fa-f:.\[\]]+`)

// accessLink закрывает ссылку целиком после схемы. У некоторых форматов
// секрет стоит перед @, у VMess и старого Shadowsocks он занимает всё тело,
// а у Hysteria2 и WireGuard бывает ещё и в параметрах. Разбирать их здесь по
// отдельности опаснее: новый вариант ссылки легко оставит пароль снаружи.
// Граница слова перед схемой — чтобы «wss://» не читался как «ss://» и
// адрес WebSocket не пропадал из журнала.
var accessLink = regexp.MustCompile(`(?i)\b(?:marvia|veil-account|vless|trojan|ss|vmess|hysteria2|hy2|wireguard|wg)://[^\s"'<>]+`)

// В обычном HTTPS секретом бывает пользователь перед @, токен подписки в
// пути и парольный параметр. Путь подписки раньше задавал продавец, поэтому
// надёжно узнать его по имени нельзя: оставляем только имя панели.
var (
	httpLink         = regexp.MustCompile(`(?i)https?://[^\s"'<>]+`)
	httpUser         = regexp.MustCompile(`(?i)(https?://)[^/@\s"'<>]+@`)
	subscriptionPath = regexp.MustCompile(`(?i)(/sub/)[^/?#\s"'<>]+`)
	secretQuery      = regexp.MustCompile(`(?i)([?&](?:token|password|uuid|auth|key|secret|obfs-password|presharedkey)=)[^&#\s"'<>]+`)

	// Прежний путь подписки задавал продавец, поэтому по одному сегменту его
	// не узнать. В строке HTTP-запроса безопаснее скрыть путь целиком: метод и
	// причина ошибки для диагностики остаются, токен — нет.
	requestPath = regexp.MustCompile(`(?i)\b(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS|CONNECT|TRACE)[ \t]+(/[^\s"'<>]*)`)
)

// Addresses заменяет в строке все IP-адреса (с портом и без) пометкой.
//
// Время «12:30:45», версия «0.13.0» и прочие похожие на адрес строки не
// трогаются: их net.ParseIP не примет.
func Addresses(s string) string {
	return candidate.ReplaceAllStringFunc(s, func(m string) string {
		if isAddress(m) {
			return Placeholder
		}
		// Знак препинания после адреса — часть фразы, а не адреса:
		// «from 203.0.113.7:51234: EOF», «к 203.0.113.7.».
		core := strings.TrimRight(m, ":.")
		if isAddress(core) {
			return Placeholder + m[len(core):]
		}
		return m
	})
}

// Secrets убирает из текста ключи и пароли ссылок доступа.
func Secrets(s string) string {
	s = accessLink.ReplaceAllStringFunc(s, func(link string) string {
		// Даже последний знак может быть частью пароля. Поэтому закрываем всё
		// до пробела, а не пытаемся сохранить пунктуацию вокруг ссылки.
		if i := strings.Index(link, "://"); i >= 0 {
			return link[:i+3] + SecretPlaceholder
		}
		return SecretPlaceholder
	})
	s = httpLink.ReplaceAllStringFunc(s, func(link string) string {
		i := strings.Index(link, "://")
		if i < 0 {
			return SecretPlaceholder
		}
		prefix, rest := link[:i+3], link[i+3:]
		boundary := strings.IndexAny(rest, "/?#")
		authority := rest
		hasPath := boundary >= 0
		if hasPath {
			authority = rest[:boundary]
		}
		if at := strings.LastIndexByte(authority, '@'); at >= 0 {
			authority = SecretPlaceholder + "@" + authority[at+1:]
		}
		if authority == "" {
			authority = SecretPlaceholder
		}
		if hasPath {
			return prefix + authority + "/" + SecretPlaceholder
		}
		return prefix + authority
	})
	s = httpUser.ReplaceAllString(s, "${1}"+SecretPlaceholder+"@")
	s = subscriptionPath.ReplaceAllString(s, "${1}"+SecretPlaceholder)
	s = secretQuery.ReplaceAllString(s, "${1}"+SecretPlaceholder)
	s = requestPath.ReplaceAllString(s, "${1} <путь>")
	return s
}

// Text готовит произвольную строку для журнала сервера.
func Text(s string) string {
	return Addresses(Secrets(s))
}

func isAddress(s string) bool {
	if !strings.ContainsAny(s, ".:") {
		return false
	}
	if host, _, err := net.SplitHostPort(s); err == nil {
		return net.ParseIP(host) != nil
	}
	return net.ParseIP(strings.Trim(s, "[]")) != nil
}

// Error — текст ошибки без адресов. nil превращается в пустую строку, а не в
// «<nil>»: в журнале это читалось бы как ещё одна ошибка.
func Error(err error) string {
	if err == nil {
		return ""
	}
	return Text(err.Error())
}

// Wrap оставляет ошибку в цепочке errors.Is и errors.As, но не даёт её
// тексту раскрыть ссылку. Адреса здесь сохраняются: в клиентской ошибке они
// помогают отличить поломку панели от отсутствия сети; серверный журнал
// дополнительно проходит через Error или Text.
func Wrap(err error) error {
	if err == nil {
		return nil
	}
	return &wrappedError{err: err}
}

type wrappedError struct{ err error }

func (e *wrappedError) Error() string { return Secrets(e.err.Error()) }
func (e *wrappedError) Unwrap() error { return e.err }

// Logger — журнал для http.Server.ErrorLog и подобных чужих писателей.
//
// Строки уходят в стандартный журнал процесса — туда же, куда пишут
// log.Printf панели и ноды, с тем же временем и в тот же journald, — но уже
// без адресов. Пустой ErrorLog у http.Server означал бы тот же стандартный
// журнал, только с адресами.
func Logger() *log.Logger {
	return log.New(writer{}, "", 0)
}

type writer struct{}

func (writer) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\n")
	if line == "" {
		return len(p), nil
	}
	if err := log.Output(2, Text(line)); err != nil {
		return 0, err
	}
	return len(p), nil
}
