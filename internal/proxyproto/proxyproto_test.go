package proxyproto

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// TestHeaderCarriesTheBuyerAddressAndNothingMore: заголовок отдаёт адрес
// покупателя, а приветствие клиента, идущее следом, остаётся в потоке
// нетронутым — его разбирает уже протокол.
func TestHeaderCarriesTheBuyerAddressAndNothingMore(t *testing.T) {
	for _, tc := range []struct{ src, dst string }{
		{"203.0.113.7:51234", "198.51.100.1:443"},
		{"[2001:db8::7]:51234", "[2001:db8::1]:443"},
	} {
		src, _ := net.ResolveTCPAddr("tcp", tc.src)
		dst, _ := net.ResolveTCPAddr("tcp", tc.dst)
		stream := bytes.NewReader(append(Header(src, dst), "\x16\x03\x01hello"...))
		got, err := Read(stream)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if got.String() != src.String() {
			t.Fatalf("адрес %s, а не %s", got, src)
		}
		rest, _ := io.ReadAll(stream)
		if string(rest) != "\x16\x03\x01hello" {
			t.Fatalf("заголовок съел данные клиента: %q", rest)
		}
	}
}

// TestGarbageIsNotTakenForAnAddress: поток не с заголовка и заголовок
// непомерной длины — ошибки, а не адрес.
func TestGarbageIsNotTakenForAnAddress(t *testing.T) {
	if _, err := Read(bytes.NewReader([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))); err == nil {
		t.Fatal("HTTP-запрос принят за заголовок")
	}
	huge := append(append([]byte(nil), signature...), cmdProxy, famTCP4, 0xff, 0xff)
	if _, err := Read(bytes.NewReader(huge)); err == nil {
		t.Fatal("заголовок на 64 КиБ принят")
	}
}

// TestOnlyTrustedPeersMayNameTheBuyer: заголовку верят только от своего
// входа. От постороннего он остаётся частью данных — иначе любой назвался бы
// чужим адресом и обошёл лимит устройств.
func TestOnlyTrustedPeersMayNameTheBuyer(t *testing.T) {
	for _, trust := range []bool{true, false} {
		inner, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ln := Listen(inner, func(net.IP) bool { return trust })

		buyer := &net.TCPAddr{IP: net.ParseIP("203.0.113.7"), Port: 40000}
		go func() {
			c, err := net.Dial("tcp", inner.Addr().String())
			if err != nil {
				return
			}
			defer c.Close()
			_, _ = c.Write(append(Header(buyer, c.RemoteAddr()), "hi"...))
			time.Sleep(200 * time.Millisecond)
		}()

		c, err := ln.Accept()
		if err != nil {
			t.Fatal(err)
		}
		head := make([]byte, 2)
		if trust {
			_, _ = io.ReadFull(c, head)
			if c.RemoteAddr().String() != buyer.String() || string(head) != "hi" {
				t.Fatalf("от входа: адрес %s, данные %q", c.RemoteAddr(), head)
			}
		} else {
			_, _ = io.ReadFull(c, head)
			if c.RemoteAddr().String() == buyer.String() || !bytes.Equal(head, signature[:2]) {
				t.Fatalf("посторонний назвался %s", c.RemoteAddr())
			}
		}
		_ = c.Close()
		_ = ln.Close()
	}
}

// TestASilentEntryDoesNotStopTheOthers: вход, открывший соединение и
// замолчавший, не держит приём остальных.
func TestASilentEntryDoesNotStopTheOthers(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ln := Listen(inner, func(net.IP) bool { return true })
	defer ln.Close()

	silent, err := net.Dial("tcp", inner.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()

	buyer := &net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 40001}
	go func() {
		c, err := net.Dial("tcp", inner.Addr().String())
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = c.Write(Header(buyer, c.RemoteAddr()))
		time.Sleep(time.Second)
	}()

	got := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			got <- c
		}
	}()
	select {
	case c := <-got:
		defer c.Close()
		if c.RemoteAddr().String() != buyer.String() {
			t.Fatalf("принят %s", c.RemoteAddr())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("молчащий вход остановил приём")
	}
}
