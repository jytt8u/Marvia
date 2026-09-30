package client

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// Ошибки, в которых лежит секрет.
//
// В ссылке доступа два секрета сразу: приватный ключ покупателя и токен
// подписки. Токен — это пароль от доступа: кто его увидел, тот и покупатель, и
// он же открывает список нод продавца. Ключ ещё дороже.
//
// Беда в том, что оба попадают в тексты ошибок сами. url.Parse и http.Client
// возвращают *url.Error, а он печатает разобранный адрес целиком:
//
//	Get "https://panel.example.com/sub/0XWt7C7KexXcIVSYLvIAU3…": dial tcp: …
//	parse "marvia://<приватный ключ>@panel…": invalid port
//
// Дальше этот текст показывается в окне и ложится в журнал — а журнал человек
// пересылает продавцу, когда просит помочь. То есть секрет уезжает в чужую
// переписку ровно в тот момент, когда человеку и так плохо.
//
// Поэтому вырезаем адрес и из вложенных ошибок. Ошибки разбора адреса
// обезличиваем: стандартная библиотека может повторить секрет в самой причине.

// withoutSecret убирает адрес из ошибки, оставляя действие, хост и причину.
//
// Хост оставляем намеренно: «панель не отвечает» без имени панели не отличить
// от «интернета нет», а в имени секрета нет — оно и так на виду у провайдера
// в запросе имён.
func withoutSecret(err error) error {
	var uerr *url.Error
	if !errors.As(err, &uerr) {
		return err
	}

	host := "адрес из ссылки доступа"
	if parsed, perr := url.Parse(uerr.URL); perr == nil && parsed.Host != "" {
		host = parsed.Host
	}

	op := uerr.Op
	if op == "" {
		op = "запрос"
	}
	return fmt.Errorf("%s %s: %w", op, host, safeURLCause(uerr.Err))
}

// withoutLink убирает ссылку целиком.
//
// Годится там, где секрет — сам разбираемый адрес: у неразобранной ссылки хост
// доверия не заслуживает, а приватный ключ в ней стоит первым.
func withoutLink(err error) error {
	var uerr *url.Error
	if !errors.As(err, &uerr) {
		return err
	}
	return safeURLCause(uerr.Err)
}

// redactedError сохраняет цепочку для errors.Is, но не печатает секрет из
// исходной причины. Журнал и интерфейс используют только безопасный Error.
type redactedError struct {
	message string
	cause   error
}

func (e *redactedError) Error() string { return e.message }
func (e *redactedError) Unwrap() error { return e.cause }

func safeURLCause(err error) error {
	var nested *url.Error
	if errors.As(err, &nested) {
		return withoutLink(err)
	}
	var escape url.EscapeError
	var invalidHost url.InvalidHostError
	if errors.As(err, &escape) || errors.As(err, &invalidHost) {
		return &redactedError{message: "неверный адрес в ссылке доступа", cause: err}
	}
	// http.Client включает Location целиком, причём относительный адрес тоже.
	// В этой ошибке url.Error вложен текстом, а не через Unwrap.
	if strings.Contains(err.Error(), "failed to parse Location header") {
		return &redactedError{message: "неверный адрес перенаправления подписки", cause: err}
	}
	return err
}
