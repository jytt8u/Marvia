package main

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/proxyproto"
)

// TestEntryHandsTheBuyerAddressToTheExit: соединение через вход доходит до
// выхода с настоящим адресом покупателя, а данные идут в обе стороны
// нетронутыми — вход только перекладывает.
func TestEntryHandsTheBuyerAddressToTheExit(t *testing.T) {
	exitInner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	exit := proxyproto.Listen(exitInner, func(ip net.IP) bool { return ip.IsLoopback() })
	defer exit.Close()

	entry, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	go func() { _ = serveRelay(entry, func() string { return exitInner.Addr().String() }) }()

	type seen struct {
		from string
		got  string
	}
	result := make(chan seen, 1)
	go func() {
		c, err := exit.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		buf := make([]byte, 5)
		_, _ = io.ReadFull(c, buf)
		_, _ = c.Write([]byte("ответ"))
		result <- seen{c.RemoteAddr().String(), string(buf)}
	}()

	buyer, err := net.Dial("tcp", entry.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer buyer.Close()
	if _, err := buyer.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	reply := make([]byte, len("ответ"))
	_ = buyer.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(buyer, reply); err != nil || string(reply) != "ответ" {
		t.Fatalf("ответ выхода: %q, %v", reply, err)
	}
	s := <-result
	if s.from != buyer.LocalAddr().String() || s.got != "hello" {
		t.Fatalf("выход увидел %s и %q, а покупатель — %s", s.from, s.got, buyer.LocalAddr())
	}
}

// TestEntryWithoutAnExitClosesInsteadOfHanging: пока панель не назвала
// выход (или выключила его), вход закрывает соединение сразу — клиент
// уходит на другую ноду, а не ждёт до своего таймаута.
func TestEntryWithoutAnExitClosesInsteadOfHanging(t *testing.T) {
	entry, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer entry.Close()
	go func() { _ = serveRelay(entry, func() string { return "" }) }()

	c, err := net.Dial("tcp", entry.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("вход без выхода не закрыл соединение: %v", err)
	}
}
