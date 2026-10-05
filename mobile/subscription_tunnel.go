package mobile

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/jytt8u/marvia/internal/tunbridge"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Subscription обновляет любую сохранённую подписку через живой туннель.
// Приложение исключено из системного VPN ради соединений с нодами, поэтому
// обычный Mobile.Subscription пошёл бы мимо него. При отказе туннеля не
// откатываемся на прямую сеть: показываем кэш и повторяем запрос позже.
func (t *Tunnel) Subscription(accountLink, cacheDir string, refresh bool) (string, error) {
	t.mu.Lock()
	backend := t.dialer
	running := t.running
	t.mu.Unlock()
	if backend == nil || !running {
		return "", fail(FailPanel, errors.New("туннель уже остановлен"))
	}
	dialer := tunbridge.Metered(backend, &t.up, &t.down)
	transport := subscriptionTransport(dialer.DialTarget)
	defer transport.CloseIdleConnections()
	return subscription(accountLink, cacheDir, refresh, &http.Client{Transport: transport})
}

func subscriptionTransport(dial func(context.Context, vp1.Address) (net.Conn, error)) *http.Transport {
	return &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, _, address string) (net.Conn, error) {
			host, portText, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil {
				return nil, err
			}
			// Имя уходит ноде вместе с запросом; системный резолвер телефона
			// не нужен. Стандартный TLS по-прежнему проверяет имя панели.
			target, err := vp1.AddressFromHostPort(host, uint16(port))
			if err != nil {
				return nil, err
			}
			return dial(ctx, target)
		},
	}
}
