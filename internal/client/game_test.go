package client_test

import (
	"context"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/vp1"
)

// Игровой режим обещает одно: UDP игры не делит соединение до ноды с
// остальным трафиком. Проверяем по рукопожатиям, которые видит нода, — это
// и есть число соединений.

func gameDialer(t *testing.T, game bool) (*client.Dialer, *testNode) {
	t.Helper()
	node := startTestNode(t)
	opts := node.opts
	opts.Game = game
	dialer, err := client.NewDialer(node.info, node.clientKey, opts)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	t.Cleanup(func() { _ = dialer.Close() })
	return dialer, node
}

// echoOnce шлёт датаграмму в эхо и ждёт её обратно через ноду.
func echoOnce(t *testing.T, dialer *client.Dialer) {
	t.Helper()
	target := echoUDP(t)
	addr, err := vp1.AddressFromHostPort(target.IP.String(), uint16(target.Port))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	stream, err := dialer.DialDatagrams(ctx, addr)
	if err != nil {
		t.Fatalf("поток датаграмм: %v", err)
	}
	defer stream.Close()
	if _, err := stream.Write([]byte("ход")); err != nil {
		t.Fatalf("отправка: %v", err)
	}
	buf := make([]byte, 64)
	_ = stream.SetReadDeadline(time.Now().Add(10 * time.Second))
	n, err := stream.Read(buf)
	if err != nil || string(buf[:n]) != "эхо:ход" {
		t.Fatalf("ответ %q, %v", buf[:n], err)
	}
}

func fetch(t *testing.T, dialer *client.Dialer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := dialer.MeasureFetch(ctx, 4096); err != nil {
		t.Fatalf("закачка: %v", err)
	}
}

// TestGameUDPRidesItsOwnConnection: в игровом режиме игра и закачка — два
// соединения до ноды, а две игры подряд — всё ещё одно игровое.
func TestGameUDPRidesItsOwnConnection(t *testing.T) {
	dialer, node := gameDialer(t, true)
	echoOnce(t, dialer)
	echoOnce(t, dialer)
	if got := node.handshakes.Load(); got != 1 {
		t.Fatalf("после двух игровых потоков соединений %d, ожидалось одно игровое", got)
	}
	fetch(t, dialer)
	if got := node.handshakes.Load(); got != 2 {
		t.Fatalf("закачка не открыла своего соединения: всего %d", got)
	}
}

// TestWithoutGameModeEverythingSharesOneConnection: без режима всё по-старому
// — лишнего соединения к ноде клиент не открывает.
func TestWithoutGameModeEverythingSharesOneConnection(t *testing.T) {
	dialer, node := gameDialer(t, false)
	echoOnce(t, dialer)
	fetch(t, dialer)
	if got := node.handshakes.Load(); got != 1 {
		t.Fatalf("без игрового режима соединений %d, ожидалось одно", got)
	}
}
