package main

import (
	"errors"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/seller"
)

// Канал продавца в окне: куда писать, где продлить, что он хочет сказать и
// о чём пора напомнить.
//
// Всё это приходит в подписке — нашей и чужой — и уже вычищено ядром (см.
// internal/seller). Здесь — только решение, что показать, и какую ссылку
// отдать системе по нажатию.

// SellerView — канал продавца так, как его видит окно.
//
// Ссылок здесь нет, есть только «есть кнопка» и «нет кнопки»: открывает их
// программа по имени, а не страница по адресу. Так любая ошибка в разметке
// окна остаётся ошибкой разметки, а не способом открыть что угодно.
type SellerView struct {
	Support  bool   `json:"support"`
	Renew    bool   `json:"renew"`
	Announce string `json:"announce,omitempty"`

	// Remind — о чём напомнить: "expiry" (RemindValue — дней до конца) или
	// "traffic" (RemindValue — процентов трафика осталось); пусто — не о
	// чем или человек это уже видел. Подробности — client.Reminder.
	Remind      string `json:"remind,omitempty"`
	RemindValue int64  `json:"remind_value,omitempty"`
	RemindKey   string `json:"remind_key,omitempty"`
}

// sellerView решает, что показать на момент now. seen — ключ напоминания,
// которое человек уже закрыл: то же самое второй раз не показываем, а
// следующая ступень («остался день») или новый срок после продления — это
// другой ключ, и он покажется.
func sellerView(sub client.Subscription, seen string, now time.Time) SellerView {
	v := SellerView{
		Support:  sub.SupportURL != "",
		Renew:    sub.RenewURL != "",
		Announce: sub.Announce,
	}
	if r := sub.Reminder(now); r.Kind != "" && r.Key != seen {
		v.Remind, v.RemindValue, v.RemindKey = r.Kind, r.Value, r.Key
	}
	return v
}

// errNoSellerLink — продавец такой ссылки не оставил.
var errNoSellerLink = errors.New("продавец не оставил такой ссылки")

// sellerLink — ссылка, которую можно отдать системе: which — "support" или
// "renew".
//
// Ядро её уже проверило, но проверяем снова, прямо перед открытием: ссылка
// уходит в проводник, а тот откроет что угодно, и цена лишней проверки —
// микросекунды. Сверх общих правил (только https:// и tg://) — без кавычки:
// в адресе её быть не должно, а в командной строке проводника она закрыла
// бы аргумент и дописала свой.
func sellerLink(sub client.Subscription, which string) (string, error) {
	var raw string
	switch which {
	case "support":
		raw = sub.SupportURL
	case "renew":
		raw = sub.RenewURL
	default:
		return "", errors.New("неизвестная ссылка продавца")
	}
	link, err := seller.CheckLink(raw)
	if err != nil {
		return "", err
	}
	if link == "" {
		return "", errNoSellerLink
	}
	if strings.ContainsRune(link, '"') {
		return "", errors.New("в ссылке продавца кавычка")
	}
	return link, nil
}
