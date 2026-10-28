package transport_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/transport"
)

// Слушатель получает внешний UDP-сокет. Закрытие библиотеки само по себе
// не возвращает порт: он принадлежит нашему транспорту.
func TestQUICListenerReturnsItsUDPPort(t *testing.T) {
	ln, _ := quicPair(t)
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	pc, err := net.ListenPacket("udp4", addr)
	if err != nil {
		t.Fatalf("порт остался занят после остановки ноды: %v", err)
	}
	_ = pc.Close()
}

func TestQUICClientReturnsItsUDPPortAfterClose(t *testing.T) {
	ln, roots := quicPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, err := transport.DialQUIC(ctx, ln.Addr().String(), transport.QUICDialConfig{
		TLS: transport.ClientConfig{ServerName: "localhost", RootCAs: roots},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	addr := conn.LocalAddr().String()
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	// Пять секунд нужны собеседнику для последнего ответа. После этого
	// сокет должен освободиться, а не жить до выхода из приложения.
	deadline := time.Now().Add(8 * time.Second)
	for {
		pc, err := net.ListenPacket("udp4", addr)
		if err == nil {
			_ = pc.Close()
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("сокет клиента остался занят после закрытия: %v", err)
		}
		time.Sleep(25 * time.Millisecond)
	}
}
