// Package redact вычищает сетевые адреса из строк, уходящих в журнал.
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

// candidate — всё, что по набору символов может оказаться адресом: IPv4,
// IPv6 в квадратных скобках или без, с портом и без. Решает не выражение, а
// net.ParseIP: регулярное выражение для IPv6 целиком вышло бы и длинным, и
// неверным, а разбор адреса в стандартной библиотеке уже есть.
var candidate = regexp.MustCompile(`[0-9A-Fa-f:.\[\]]+`)

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
	return Addresses(err.Error())
}

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
	if err := log.Output(2, Addresses(line)); err != nil {
		return 0, err
	}
	return len(p), nil
}
