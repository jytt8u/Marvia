package tunbridge_test

import (
	"net"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/checksum"
	"gvisor.dev/gvisor/pkg/tcpip/header"

	"github.com/jytt8u/marvia/internal/tunbridge"
)

// syn6 — IPv6 SYN от приложения к сайту на Fastly.
func syn6(src, dst tcpip.Address) []byte {
	b := make([]byte, header.IPv6MinimumSize+header.TCPMinimumSize)
	ip := header.IPv6(b)
	ip.Encode(&header.IPv6Fields{
		PayloadLength: header.TCPMinimumSize, TransportProtocol: header.TCPProtocolNumber,
		HopLimit: 64, SrcAddr: src, DstAddr: dst,
	})
	tcp := header.TCP(b[header.IPv6MinimumSize:])
	tcp.Encode(&header.TCPFields{SrcPort: 40001, DstPort: 443, SeqNum: 7000, DataOffset: header.TCPMinimumSize, Flags: header.TCPFlagSyn, WindowSize: 65535})
	xsum := header.PseudoHeaderChecksum(header.TCPProtocolNumber, src, dst, header.TCPMinimumSize)
	tcp.SetChecksum(^checksum.Checksum(tcp, xsum))
	return b
}

// TestIPv6IsRefusedAtTheSynWhenItIsOff: IPv6 выключен — соединение по нему
// получает отказ (RST) на первый же SYN, а не рукопожатие с обрывом следом.
// После рукопожатия браузер считал сайт ответившим, писал
// ERR_CONNECTION_CLOSED и на IPv4 не переходил: так не грузились картинки
// GitHub, у которых есть IPv6-адрес.
func TestIPv6IsRefusedAtTheSynWhenItIsOff(t *testing.T) {
	device, peer := net.Pipe()
	dialer := pipeDialer{far: make(chan net.Conn, 1)}
	bridge, err := tunbridge.Start(tunbridge.Config{Device: device, MTU: 1500, Dialer: dialer, DNS: "1.1.1.1:53", NoIPv6: true})
	if err != nil {
		t.Fatalf("запуск моста: %v", err)
	}
	t.Cleanup(func() { _ = bridge.Close(); _ = peer.Close() })

	replies := make(chan header.TCP, 8)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := peer.Read(buf)
			if err != nil {
				return
			}
			ip := header.IPv6(append([]byte(nil), buf[:n]...))
			if n >= header.IPv6MinimumSize && ip.TransportProtocol() == header.TCPProtocolNumber {
				replies <- header.TCP(ip.Payload())
			}
		}
	}()

	src := tcpip.AddrFrom16([16]byte{0xfd, 0, 0x6d, 0x61, 0x72, 0x76, 15: 1})
	dst := tcpip.AddrFrom16([16]byte{0x2a, 0x04, 0x4e, 0x42, 15: 0x84})
	// Первый SYN мог прийти, пока мост ещё собирался, — система повторяет.
	deadline := time.After(5 * time.Second)
	for {
		_, _ = peer.Write(syn6(src, dst))
		select {
		case r := <-replies:
			if r.Flags()&header.TCPFlagRst == 0 {
				t.Fatalf("на IPv6 SYN мост ответил %v, а не отказом", r.Flags())
			}
			return
		case <-time.After(200 * time.Millisecond):
		case <-deadline:
			t.Fatal("на IPv6 SYN мост не ответил ничем")
		}
	}
}
