package nodesync

import (
	"net"
	"sync"
)

// Каскад: вход в стране покупателя и зарубежная нода-выход.
//
// Панель говорит входу одно — куда перекладывать байты, а выходу одно —
// адреса его входов, от которых верить заголовку с адресом покупателя
// (internal/proxyproto). Ни ключей, ни списка покупателей вход не получает:
// изъятый вход выдаёт только адрес выхода, который и так виден по его
// трафику.

// route — что панель сказала про каскад. Пишет цикл синхронизации, читают
// соединения, поэтому под замком.
type route struct {
	mu     sync.RWMutex
	to     string
	relays map[string]bool
}

func (r *route) set(to string, relays []string) {
	set := make(map[string]bool, len(relays))
	for _, s := range relays {
		if ip := net.ParseIP(s); ip != nil {
			set[ip.String()] = true
		}
	}
	r.mu.Lock()
	r.to, r.relays = to, set
	r.mu.Unlock()
}

func (r *route) get() (string, []string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]string, 0, len(r.relays))
	for s := range r.relays {
		list = append(list, s)
	}
	return r.to, list
}

// RelayTo — куда входу перекладывать соединения; пусто — никуда: панель ещё
// не ответила, вход выключен или токен отозван.
func (c *Client) RelayTo() string {
	c.route.mu.RLock()
	defer c.route.mu.RUnlock()
	return c.route.to
}

// TrustsRelay говорит, вход ли это ноды: только от входов нода принимает
// адрес покупателя.
func (c *Client) TrustsRelay(ip net.IP) bool {
	c.route.mu.RLock()
	defer c.route.mu.RUnlock()
	return c.route.relays[ip.String()]
}
