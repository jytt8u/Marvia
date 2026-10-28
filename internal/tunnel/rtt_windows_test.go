package tunnel

import (
	"io"
	"net"
	"testing"
	"time"
)

// Структуру TCP_INFO_v0 мы описали сами, по mstcpip.h. Если хоть одно поле
// съехало, остальные читаются со сдвигом, и отклик на экране — случайное
// число. Проверяем на живом соединении всё, что можно сверить.
func TestWindowsTCPInfoLayoutMatchesTheSystem(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			defer c.Close()
			_, _ = io.Copy(c, c)
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	const payload = 64 << 10
	if _, err := conn.Write(make([]byte, payload)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(conn, make([]byte, payload)); err != nil {
		t.Fatal(err)
	}

	raw, err := conn.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	info, ok := tcpInfo(raw)
	if !ok {
		t.Skip("SIO_TCP_INFO недоступен: Windows старее 1703")
	}
	if info.State != tcpStateEstablished {
		t.Errorf("состояние %d, а соединение установлено", info.State)
	}
	if info.Mss < 500 || info.Mss > 70000 {
		t.Errorf("MSS %d — поле прочитано не оттуда", info.Mss)
	}
	if info.BytesOut < payload || info.BytesIn < payload {
		t.Errorf("отправлено %d, принято %d, а прошло по %d", info.BytesOut, info.BytesIn, payload)
	}
	if info.RttUs > uint32(time.Second/time.Microsecond) {
		t.Errorf("отклик петли %d мкс — поле прочитано не оттуда", info.RttUs)
	}

	rtt, ok, err := kernelRTT(conn.(*net.TCPConn), &rttState{})
	if !ok || err != nil || rtt <= 0 || rtt > time.Second {
		t.Fatalf("отклик: %v, %v, %v", rtt, ok, err)
	}
}
