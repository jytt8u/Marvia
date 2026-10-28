//go:build windows

package main

import (
	"errors"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/foreign"
)

// remindedSetting — где лежит ключ последнего закрытого напоминания.
const remindedSetting = "reminded"

// trayNotifiedSetting — ключ последнего напоминания, показанного
// уведомлением. Отдельно от закрытого в окне: всплыть уведомлением и быть
// прочитанным — разные вещи, и одно не должно гасить другое.
const trayNotifiedSetting = "tray-reminded"

// TrayReminder отдаёт заголовок и текст уведомления, если пора напомнить и
// это ещё не всплывало. Показанное запоминается на диске: автозапуск каждое
// утро не должен повторять вчерашнее.
func (c *Controller) TrayReminder(now time.Time) (title, text string, ok bool) {
	c.mu.Lock()
	sub := c.subscriptionLocked()
	closed := c.reminded
	c.mu.Unlock()
	r, show := trayReminder(sub, closed, readUISetting(trayNotifiedSetting), now)
	if !show {
		return "", "", false
	}
	if err := writeUISetting(trayNotifiedSetting, r.Key); err != nil {
		// Не запомнили — не показываем: иначе оно всплывало бы каждые
		// полчаса до конца срока.
		return "", "", false
	}
	switch r.Kind {
	case client.ReminderExpiry:
		text = sayf("trayRemindExpiry", r.Value)
	case client.ReminderTraffic:
		text = sayf("trayRemindTraffic", r.Value)
	default:
		return "", "", false
	}
	return say("trayRemindTitle"), text, true
}

// knownSubscription — что известно о подписке ключа без похода в сеть: из
// кэша, который оставило последнее подключение или последняя попытка.
//
// Без сети намеренно. «Продлить» нужнее всего ровно тогда, когда
// подключиться нельзя: доступ кончился, и панель, может быть, тоже не
// отвечает. Подключение кэш обновляет само — и при неудаче тоже, до отказа
// по сроку, — так что после неудачной попытки здесь уже свежие ссылки.
func knownSubscription(link string) client.Subscription {
	if link == "" {
		return client.Subscription{}
	}
	if foreign.IsForeign(link) {
		dir, err := settingsDir()
		if err != nil {
			return client.Subscription{}
		}
		sub, ok := foreign.Cached(link, foreign.CachePath(dir, link))
		if !ok {
			return client.Subscription{}
		}
		return foreign.View(sub)
	}
	account, err := client.ParseAccountLink(link)
	if err != nil {
		return client.Subscription{}
	}
	cached, err := client.LoadCache(cachePathFor(account.SubscriptionURL), account.SubscriptionURL)
	if err != nil {
		return client.Subscription{}
	}
	return cached.Subscription
}

// subscriptionLocked — подписка, о которой сейчас говорит окно: живая, пока
// поднят туннель, иначе последняя известная. Вызывать под c.mu.
func (c *Controller) subscriptionLocked() client.Subscription {
	if c.dialer != nil {
		return c.dialer.Subscription()
	}
	return c.known
}

// rememberSubscription перечитывает кэш подписки для ключа link, если ключ
// за это время не сменили.
func (c *Controller) rememberSubscription(link string) {
	sub := knownSubscription(link)
	c.mu.Lock()
	if c.account == link {
		c.known = sub
	}
	c.mu.Unlock()
}

// OpenSeller открывает ссылку продавца — "support" или "renew" — в браузере
// или телеграме.
//
// Адрес берётся из подписки, а не от страницы окна: страница называет
// только кнопку. Иначе любая ошибка в разметке превращалась бы в открытие
// чего угодно.
func (c *Controller) OpenSeller(which string) error {
	c.mu.Lock()
	sub := c.subscriptionLocked()
	c.mu.Unlock()
	link, err := sellerLink(sub, which)
	if errors.Is(err, errNoSellerLink) {
		return errors.New(say("sellerNoLink"))
	}
	if err != nil {
		// Подробность — в журнал: человеку она ничего не скажет, а вот
		// продавцу, которому он пожалуется, — скажет.
		c.log.add("%s", sayf("logSellerBadLink", err))
		return errors.New(say("sellerBadLink"))
	}
	return openLink(link)
}

// SeenReminder запоминает, что человек закрыл напоминание с ключом key: то
// же самое второй раз не покажется, а следующая ступень — покажется.
//
// Хранится на диске, а не в памяти: иначе каждое утро автозапуск показывал
// бы закрытое вчера «осталось три дня» заново.
func (c *Controller) SeenReminder(key string) error {
	key = strings.TrimSpace(key)
	c.mu.Lock()
	current := c.subscriptionLocked().Reminder(time.Now()).Key
	c.mu.Unlock()
	// Закрыть можно только то, что сейчас показано: ключ — не место для
	// произвольной строки от страницы, он ляжет в файл.
	if key == "" || key != current {
		return nil
	}
	if err := writeUISetting(remindedSetting, key); err != nil {
		return err
	}
	c.mu.Lock()
	c.reminded = key
	c.mu.Unlock()
	return nil
}
