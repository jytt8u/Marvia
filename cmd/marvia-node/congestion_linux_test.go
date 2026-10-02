package main

import (
	"net"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// Алгоритм ставится на слушающий сокет, а работать должен на принятых
// соединениях. Проверяем наследование на reno: его ядро разрешает любому
// процессу, а по умолчанию стоит cubic — значит, reno на принятом
// соединении взялся именно от слушающего сокета. BBR на сборочной машине
// может не быть, механизм от этого не меняется.
func TestAcceptedConnectionsInheritCongestionControl(t *testing.T) {
	ln, err := listenWithCongestion("127.0.0.1:0", "reno")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		if c, err := net.Dial("tcp", ln.Addr().String()); err == nil {
			defer c.Close()
			_, _ = c.Read(make([]byte, 1))
		}
	}()
	conn, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	raw, err := conn.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var (
		algo   string
		getErr error
	)
	if err := raw.Control(func(fd uintptr) {
		algo, getErr = unix.GetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_CONGESTION)
	}); err != nil || getErr != nil {
		t.Fatalf("чтение алгоритма: %v %v", err, getErr)
	}
	if got := strings.TrimRight(algo, "\x00"); got != "reno" {
		t.Fatalf("на принятом соединении %q, а на слушающем сокете стоял reno", got)
	}
}
