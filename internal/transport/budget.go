package transport

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// Бюджет рукопожатий к одной ноде.
//
// По наблюдениям 2025–2026 годов (net4people #546, Xray-core #6293) ТСПУ
// домашних провайдеров замораживает адрес, к которому в коротком окне ушло
// больше трёх TLS-рукопожатий: соединения молча дропаются минуту-две, а
// повторная попытка с другим отпечатком удлиняет штраф. Пул сам держит не
// больше двух сессий, но рукопожатия к той же ноде рождаются и в других
// местах: замер по кнопке «Обновить», переподключение после смены сети,
// ротация сессий. Каждое по отдельности невинно, вместе они и дают всплеск.
//
// Поэтому бюджет общий на процесс и считается по адресу ноды, а не по
// месту в коде: кто бы ни звонил, четвёртое рукопожатие за минуту ждёт, пока
// освободится место. Цена — подключение изредка ждёт до минуты; без этого
// адрес замерзает на те же минуту-две, только ещё и для всех соединений сразу.
const (
	handshakesPerWindow = 3
	handshakeWindow     = time.Minute
)

// ErrHandshakeBudget — к ноде уже было столько рукопожатий, что новое
// дождётся места позже, чем истечёт срок у вызвавшего.
var ErrHandshakeBudget = errors.New("к ноде уже было три рукопожатия за минуту: новое отложено, чтобы адрес не заморозили")

type handshakeBudget struct {
	mu   sync.Mutex
	seen map[string][]time.Time
	now  func() time.Time
}

var budget = &handshakeBudget{seen: map[string][]time.Time{}, now: time.Now}

// take занимает место под рукопожатие к addr, при нужде дожидаясь его.
//
// Если места не будет до дедлайна ctx, ждать незачем: возвращается
// ErrHandshakeBudget сразу, и замер узнаёт об этом за миг, а не через восемь
// секунд тайм-аута.
func (b *handshakeBudget) take(ctx context.Context, addr string) error {
	host := hostOf(addr)
	if exempt(host) {
		return nil
	}
	for {
		wait := b.reserve(host)
		if wait <= 0 {
			return nil
		}
		if deadline, ok := ctx.Deadline(); ok && b.now().Add(wait).After(deadline) {
			return ErrHandshakeBudget
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// reserve записывает рукопожатие и возвращает ноль либо говорит, сколько
// ждать до освобождения места.
func (b *handshakeBudget) reserve(host string) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	recent := b.seen[host][:0]
	for _, at := range b.seen[host] {
		if now.Sub(at) < handshakeWindow {
			recent = append(recent, at)
		}
	}
	if len(recent) < handshakesPerWindow {
		b.seen[host] = append(recent, now)
		return 0
	}
	b.seen[host] = recent
	return recent[0].Add(handshakeWindow).Sub(now)
}

func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// exempt — петля. Это стенд и тесты, а не нода за ТСПУ, и считать
// рукопожатия к ней значило бы только тормозить проверки.
func exempt(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
