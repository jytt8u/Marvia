// Package httpguard защищает загрузку подписок, в адресах которых лежат пароли.
package httpguard

import (
	"errors"
	"net/http"
)

// SubscriptionClient сохраняет транспорт и настройки клиента, но запрещает
// потерю TLS при перенаправлении. Начальный HTTP оставлен для старых подписок:
// уже защищённый запрос нельзя молча превратить в открытый.
func SubscriptionClient(base *http.Client) *http.Client {
	client := *base
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			for _, previous := range via {
				if previous.URL.Scheme == "https" {
					return errors.New("подписка перенаправляет защищённый запрос на незашифрованный адрес")
				}
			}
		}
		if base.CheckRedirect != nil {
			return base.CheckRedirect(req, via)
		}
		if len(via) >= 10 {
			return errors.New("слишком много перенаправлений подписки")
		}
		return nil
	}
	return &client
}
