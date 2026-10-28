package client_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/transport"
	"github.com/jytt8u/marvia/internal/vp1"
)

// TestTCPOnlySendsNothingOverUDP — «только TCP» значит ни одного пакета по
// UDP, даже к ноде, которая объявила QUIC.
//
// Человек включает это там, где UDP душат: пробная попытка QUIC в такой сети
// дозванивается и потом еле тянет, и откат на TCP её не ловит. Если бы клиент
// всё равно «на всякий случай» стучался по UDP, выбор человека ничего бы не
// менял.
func TestTCPOnlySendsNothingOverUDP(t *testing.T) {
	static, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи ноды: %v", err)
	}
	cert, err := transport.SelfSignedCertificate("localhost")
	if err != nil {
		t.Fatalf("сертификат: %v", err)
	}
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("слушатель: %v", err)
	}
	ln := transport.Listen(raw, transport.ServerConfig{Certificate: cert})
	t.Cleanup(func() { _ = ln.Close() })

	// Тот же порт по UDP: сюда пришёл бы первый пакет QUIC.
	udp, err := net.ListenPacket("udp", raw.Addr().String())
	if err != nil {
		t.Skipf("UDP на том же порту не занять: %v", err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	knocked := make(chan struct{}, 1)
	go func() {
		buf := make([]byte, 2048)
		if _, _, err := udp.ReadFrom(buf); err == nil {
			knocked <- struct{}{}
		}
	}()

	greeted := make(chan struct{}, 1)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				tunnel, _, err := vp1.ServerHandshake(conn, static, vp1.NewReplayGuard(vp1.ClockSkew), vp1.AllowAll)
				if err != nil {
					_ = conn.Close()
					return
				}
				select {
				case greeted <- struct{}{}:
				default:
				}
				_ = tunnel.Close()
			}()
		}
	}()

	key, err := vp1.GenerateKeyPair()
	if err != nil {
		t.Fatalf("ключи клиента: %v", err)
	}
	node := client.Node{
		Name:      "проверочная",
		Address:   raw.Addr().String(),
		SNI:       "localhost",
		PublicKey: vp1.EncodeKey(static.Public),
		QUIC:      true,
	}
	dialer, err := client.NewDialer(node, key, client.Options{InsecureSkipVerify: true, NoQUIC: true})
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer dialer.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, _ = dialer.DialTarget(ctx, vp1.Address{Host: "example.org", Port: 443})

	select {
	case <-greeted:
	case <-time.After(8 * time.Second):
		t.Fatal("нода не дождалась клиента по TCP")
	}
	select {
	case <-knocked:
		t.Fatal("при «только TCP» клиент всё равно постучался по UDP")
	case <-time.After(300 * time.Millisecond):
	}
}
