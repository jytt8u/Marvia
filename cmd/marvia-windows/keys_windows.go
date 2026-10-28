//go:build windows

package main

import (
	"errors"
	"os"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/foreign"
)

// Keys отдаёт сохранённые ключи для окна — без самих ссылок.
func (c *Controller) Keys() []KeyView {
	c.mu.Lock()
	defer c.mu.Unlock()
	return keyViews(c.keys, c.account)
}

// UseKey делает ключ рабочим.
//
// Поднятый туннель переподнимается: ключ другой, а с ним и ноды, и продавец.
// Оставить туннель на старом ключе, показав в окне новый, значило бы врать
// о том, через чьи серверы идёт трафик.
func (c *Controller) UseKey(id string) error {
	c.mu.Lock()
	i := findKey(c.keys, id)
	if i < 0 {
		c.mu.Unlock()
		return errors.New(say("keyGone"))
	}
	link := c.keys[i].Link
	same := link == c.account
	up := c.state != StateIdle && c.state != StateFailed
	c.mu.Unlock()
	if same {
		return nil
	}

	if err := writeAccount(link); err != nil {
		return err
	}
	c.mu.Lock()
	c.account = link
	c.mu.Unlock()
	c.rememberSubscription(link)
	c.log.add("%s", sayf("logKeyUsed", c.keyName(id)))

	if up {
		c.Disconnect()
		return c.Connect()
	}
	return nil
}

// RemoveKey убирает ключ и его кэш.
//
// Убрали рабочий — остаёмся без ключа, а не с соседним втихую, как на
// телефоне: выбрать, через чьи серверы идти дальше, должен человек. Туннель
// на убранном ключе при этом опускается — держать трафик на ключе, которого
// в списке больше нет, значит показывать одно, а делать другое.
func (c *Controller) RemoveKey(id string) error {
	dir, err := settingsDir()
	if err != nil {
		return err
	}
	c.mu.Lock()
	i := findKey(c.keys, id)
	if i < 0 {
		c.mu.Unlock()
		return nil
	}
	gone := c.keys[i]
	keys := withoutKey(c.keys, id)
	active := gone.Link == c.account
	c.mu.Unlock()

	if err := saveKeys(dir, keys); err != nil {
		return err
	}
	if active {
		c.Disconnect()
		if err := writeAccount(""); err != nil {
			return err
		}
	}
	c.mu.Lock()
	c.keys = keys
	if active {
		c.account = ""
		c.known = client.Subscription{}
	}
	c.mu.Unlock()

	// Список нод убранного продавца на компьютере оставаться не должен.
	if foreign.IsForeign(gone.Link) {
		if path := foreign.CachePath(dir, gone.Link); path != "" {
			_ = os.Remove(path)
		}
	} else if account, err := client.ParseAccountLink(gone.Link); err == nil {
		if path := client.CacheFile(dir, account.SubscriptionURL); path != "" {
			_ = os.Remove(path)
		}
	}
	c.log.add("%s", sayf("logKeyRemoved", gone.Name))
	return nil
}

func (c *Controller) keyName(id string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if i := findKey(c.keys, id); i >= 0 {
		return c.keys[i].Name
	}
	return id
}
