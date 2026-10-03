package transport

import (
	"context"
	"errors"
	"testing"
	"time"
)

func freshBudget(now *time.Time) *handshakeBudget {
	return &handshakeBudget{seen: map[string][]time.Time{}, now: func() time.Time { return *now }}
}

func TestFourthHandshakeToOneNodeWithinAMinuteWaits(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	b := freshBudget(&now)
	for i := 0; i < handshakesPerWindow; i++ {
		if wait := b.reserve("198.51.100.7"); wait != 0 {
			t.Fatalf("рукопожатие %d ждёт %v, хотя бюджет не выбран", i+1, wait)
		}
		now = now.Add(5 * time.Second)
	}
	// Первое было 15 секунд назад — место освободится через 45.
	if wait := b.reserve("198.51.100.7"); wait != 45*time.Second {
		t.Fatalf("четвёртое за минуту ждёт %v, ожидалось 45 с", wait)
	}
	// Соседняя нода живёт своим бюджетом.
	if wait := b.reserve("203.0.113.9"); wait != 0 {
		t.Fatalf("другая нода ждёт %v из-за чужих рукопожатий", wait)
	}
	now = now.Add(46 * time.Second)
	if wait := b.reserve("198.51.100.7"); wait != 0 {
		t.Fatalf("через минуту после первого ждёт %v", wait)
	}
}

func TestHandshakeThatCannotFitTheDeadlineFailsAtOnce(t *testing.T) {
	now := time.Now()
	b := freshBudget(&now)
	for i := 0; i < handshakesPerWindow; i++ {
		b.reserve("198.51.100.7")
	}
	// Замер ждёт восемь секунд, а место будет через минуту: ждать незачем.
	ctx, cancel := context.WithDeadline(context.Background(), now.Add(8*time.Second))
	defer cancel()
	start := time.Now()
	if err := b.take(ctx, "198.51.100.7:443"); !errors.Is(err, ErrHandshakeBudget) {
		t.Fatalf("ошибка %v, ожидался отказ по бюджету", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("отказ занял %v — ждал впустую", took)
	}
}

func TestLoopbackIsNotCounted(t *testing.T) {
	now := time.Now()
	b := freshBudget(&now)
	for i := 0; i < 10; i++ {
		for _, addr := range []string{"127.0.0.1:443", "[::1]:443", "localhost:8443"} {
			if err := b.take(context.Background(), addr); err != nil {
				t.Fatalf("%s: %v — петля попала под бюджет", addr, err)
			}
		}
	}
}
