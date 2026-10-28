package securedns

import (
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

// Кэш ответов.
//
// Без него каждый вопрос — круг до ноды и от неё до резолвера, а приложения
// спрашивают одно и то же десятками: лента, открывая каждую карточку,
// заново спрашивает адрес своего CDN. Системный кэш на телефоне есть, но
// живёт он у каждого приложения свой и часто короче TTL.
//
// Ответ хранится не дольше, чем разрешил резолвер (наименьший TTL записей), и
// отдаётся с TTL, уменьшенным на прошедшее время: иначе приложение держало бы
// у себя адрес вдвое дольше, чем ему сказали.

const (
	// maxTTL — дольше не храним, что бы ни сказал резолвер. Час: адреса CDN
	// живут минуты, а ошибочный ответ, застрявший на сутки, — это сайт,
	// который сутки «не работает только у меня».
	maxTTL = time.Hour

	// negativeTTL — сколько помним «такого имени нет», когда резолвер не
	// приложил записи SOA со своим сроком. Полминуты хватает, чтобы не
	// спрашивать опечатку на каждое нажатие, и мало, чтобы новое имя
	// появилось быстро.
	negativeTTL = 30 * time.Second

	// maxEntries — больше записей не держим. Телефон за день спрашивает
	// несколько тысяч разных имён; дальше кэш не ускоряет, а только ест
	// память процесса, которую Android считает очень внимательно.
	maxEntries = 4096
)

type cached struct {
	msg     dnsmessage.Message
	stored  time.Time
	expires time.Time
}

type cache struct {
	mu    sync.Mutex
	items map[string]cached
}

// get отдаёт ответ с TTL, уменьшенным на прошедшее время.
func (c *cache) get(key string, now time.Time) (dnsmessage.Message, bool) {
	if key == "" {
		return dnsmessage.Message{}, false
	}
	c.mu.Lock()
	e, ok := c.items[key]
	c.mu.Unlock()
	if !ok || !now.Before(e.expires) {
		return dnsmessage.Message{}, false
	}
	age := uint32(now.Sub(e.stored) / time.Second)
	m := e.msg
	m.Answers = aged(e.msg.Answers, age)
	m.Authorities = aged(e.msg.Authorities, age)
	m.Additionals = aged(e.msg.Additionals, age)
	return m, true
}

// put кладёт ответ, если его можно хранить.
func (c *cache) put(key string, m dnsmessage.Message, now time.Time) {
	if key == "" {
		return
	}
	ttl, ok := lifetime(m)
	if !ok {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.items == nil {
		c.items = make(map[string]cached)
	}
	if len(c.items) >= maxEntries {
		for k, e := range c.items {
			if !now.Before(e.expires) {
				delete(c.items, k)
			}
		}
		// Всё ещё полон — начинаем заново. Выбирать, что выкинуть, ради
		// кэша имён не стоит: промах стоит одного круга до резолвера.
		if len(c.items) >= maxEntries {
			clear(c.items)
		}
	}
	c.items[key] = cached{msg: m, stored: now, expires: now.Add(ttl)}
}

// reset забывает всё. Зовётся при переезде на другую ноду: резолвер видит
// адрес ноды и отвечает адресами CDN рядом с ней, а нода теперь другая.
func (c *cache) reset() {
	c.mu.Lock()
	clear(c.items)
	c.mu.Unlock()
}

// lifetime — сколько можно хранить ответ. Хранится только ответ по существу:
// «вот адреса» или «такого имени нет». Отказ резолвера, обрезанный ответ и
// нулевой TTL не хранятся — их надо спросить снова.
func lifetime(m dnsmessage.Message) (time.Duration, bool) {
	if m.Header.Truncated {
		return 0, false
	}
	if m.Header.RCode != dnsmessage.RCodeSuccess && m.Header.RCode != dnsmessage.RCodeNameError {
		return 0, false
	}
	least, found := maxTTL, false
	for _, part := range [][]dnsmessage.Resource{m.Answers, m.Authorities, m.Additionals} {
		for _, rr := range part {
			if rr.Header.Type == dnsmessage.TypeOPT {
				continue
			}
			found = true
			least = min(least, time.Duration(rr.Header.TTL)*time.Second)
		}
	}
	if !found {
		return negativeTTL, true
	}
	if least <= 0 {
		return 0, false
	}
	return least, true
}

// aged — копия записей с TTL, уменьшенным на age секунд. У OPT в поле TTL
// лежат флаги, а не срок, — её не трогаем.
func aged(rrs []dnsmessage.Resource, age uint32) []dnsmessage.Resource {
	if len(rrs) == 0 {
		return nil
	}
	out := make([]dnsmessage.Resource, len(rrs))
	copy(out, rrs)
	for i := range out {
		if out[i].Header.Type == dnsmessage.TypeOPT {
			continue
		}
		if out[i].Header.TTL > age {
			out[i].Header.TTL -= age
		} else {
			out[i].Header.TTL = 0
		}
	}
	return out
}
