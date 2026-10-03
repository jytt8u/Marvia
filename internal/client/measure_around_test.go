package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

// «Обновить» в списке серверов не должно звонить ноде, через которую уже
// идёт туннель: третье и четвёртое рукопожатие к одному адресу за минуту —
// повод для ТСПУ заморозить его вместе с рабочим туннелем.
func TestRefreshDoesNotRedialTheNodeCarryingTheTunnel(t *testing.T) {
	node := startTestNode(t)
	node.info.ID = 7
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	dialer, err := client.NewDialer(node.info, node.clientKey, node.opts)
	if err != nil {
		t.Fatal(err)
	}
	defer dialer.Close()
	if err := dialer.Warmup(ctx); err != nil {
		t.Fatalf("туннель: %v", err)
	}
	before := node.handshakes.Load()

	for i := 0; i < 2; i++ {
		results := client.MeasureAround(ctx, []client.Node{node.info}, dialer, node.clientKey, node.opts)
		if len(results) != 1 || !results[0].OK() || results[0].RTT <= 0 {
			t.Fatalf("замер %d: %+v", i+1, results)
		}
	}
	if got := node.handshakes.Load() - before; got != 0 {
		t.Fatalf("две проверки открыли к текущей ноде %d новых соединений", got)
	}

	// Без живого туннеля нода меряется, как раньше, — настоящим подключением.
	client.MeasureAround(ctx, []client.Node{node.info}, nil, node.clientKey, node.opts)
	if node.handshakes.Load() == before {
		t.Fatal("без туннеля замер не подключился — счётчик ничего не ловит")
	}
}
