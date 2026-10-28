package tunbridge_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/checksum"
	"gvisor.dev/gvisor/pkg/tcpip/header"

	"github.com/jytt8u/marvia/internal/tunbridge"
	"github.com/jytt8u/marvia/internal/vp1"
)

// pipeDialer отдаёт мосту один конец трубы, а другой — тесту: что мост
// перешлёт «до цели», тест прочитает.
type pipeDialer struct{ far chan net.Conn }

func (d pipeDialer) DialTarget(context.Context, vp1.Address) (net.Conn, error) {
	near, far := net.Pipe()
	d.far <- far
	return near, nil
}

func (pipeDialer) DialDatagrams(context.Context, vp1.Address) (net.Conn, error) {
	return nil, io.EOF
}

var (
	client = tcpip.AddrFrom4([4]byte{10, 66, 0, 2})
	server = tcpip.AddrFrom4([4]byte{203, 0, 113, 5})
)

type segment struct {
	seq, ack  uint32
	flags     header.TCPFlags
	ts, ecr   uint32
	noTS, md5 bool
	ttl       uint8
	payload   []byte
}

// packet собирает IPv4 и TCP так, как их собрала бы система, с честными
// контрольными суммами: иначе стек выбросил бы пакет сам и тест не проверил
// бы ничего.
func packet(s segment) []byte {
	var opts []byte
	if !s.noTS {
		opts = append(opts, 1, 1, 8, 10)
		opts = binary.BigEndian.AppendUint32(opts, s.ts)
		opts = binary.BigEndian.AppendUint32(opts, s.ecr)
	}
	if s.md5 {
		opts = append(opts, 19, 18)
		opts = append(opts, make([]byte, 16)...)
		opts = append(opts, 1, 1)
	}
	tcpLen := header.TCPMinimumSize + len(opts) + len(s.payload)
	b := make([]byte, header.IPv4MinimumSize+tcpLen)
	ttl := s.ttl
	if ttl == 0 {
		ttl = 128
	}
	ip := header.IPv4(b)
	ip.Encode(&header.IPv4Fields{
		TotalLength: uint16(len(b)), TTL: ttl, Protocol: uint8(header.TCPProtocolNumber),
		SrcAddr: client, DstAddr: server,
	})
	ip.SetChecksum(^ip.CalculateChecksum())
	tcp := header.TCP(b[header.IPv4MinimumSize:])
	tcp.Encode(&header.TCPFields{
		SrcPort: 40000, DstPort: 443, SeqNum: s.seq, AckNum: s.ack,
		DataOffset: uint8(header.TCPMinimumSize + len(opts)), Flags: s.flags, WindowSize: 65535,
	})
	copy(tcp[header.TCPMinimumSize:], opts)
	copy(tcp[header.TCPMinimumSize+len(opts):], s.payload)
	xsum := header.PseudoHeaderChecksum(header.TCPProtocolNumber, client, server, uint16(tcpLen))
	tcp.SetChecksum(^checksum.Checksum(tcp, xsum))
	return b
}

// connect поднимает мост и проходит рукопожатие; возвращает, куда писать
// пакеты, номер сервера и его метку времени, и трубу «до цели».
func connect(t *testing.T, withTS bool) (net.Conn, uint32, uint32, net.Conn) {
	t.Helper()
	device, peer := net.Pipe()
	dialer := pipeDialer{far: make(chan net.Conn, 1)}
	bridge, err := tunbridge.Start(tunbridge.Config{Device: device, MTU: 1500, Dialer: dialer, DNS: "1.1.1.1:53"})
	if err != nil {
		t.Fatalf("запуск моста: %v", err)
	}
	t.Cleanup(func() { _ = bridge.Close(); _ = peer.Close() })

	// Ответы стека читаем всё время: труба синхронная, и непрочитанный ответ
	// остановил бы стек.
	replies := make(chan header.TCP, 64)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, err := peer.Read(buf)
			if err != nil {
				return
			}
			ip := header.IPv4(append([]byte(nil), buf[:n]...))
			if ip.Protocol() == uint8(header.TCPProtocolNumber) {
				replies <- header.TCP(ip.Payload())
			}
		}
	}()

	_, _ = peer.Write(packet(segment{seq: 1000, flags: header.TCPFlagSyn, ts: 5000, noTS: !withTS}))
	var synAck header.TCP
	select {
	case synAck = <-replies:
	case <-time.After(5 * time.Second):
		t.Fatal("стек не ответил на SYN")
	}
	serverTS := synAck.ParsedOptions().TSVal
	_, _ = peer.Write(packet(segment{seq: 1001, ack: synAck.SequenceNumber() + 1, flags: header.TCPFlagAck, ts: 5001, ecr: serverTS, noTS: !withTS}))

	var far net.Conn
	select {
	case far = <-dialer.far:
	case <-time.After(5 * time.Second):
		t.Fatal("мост не пошёл к цели")
	}
	return peer, synAck.SequenceNumber() + 1, serverTS, far
}

func readTarget(t *testing.T, far net.Conn, n int) string {
	t.Helper()
	_ = far.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, n)
	if _, err := io.ReadFull(far, buf); err != nil {
		t.Fatalf("до цели ничего не дошло: %v", err)
	}
	return string(buf)
}

func TestZapretFakeHelloWithAnOldTimestampNeverReachesTheSite(t *testing.T) {
	// Так делает zapret с --dpi-desync=fake --dpi-desync-fooling=ts: перед
	// настоящим приветствием — поддельное, с меткой времени на 600000 меньше.
	stale := uint32(5001)
	stale -= 600000 // через ноль, как и у zapret: метки идут по кругу
	peer, ack, serverTS, far := connect(t, true)
	data := header.TCPFlagAck | header.TCPFlagPsh
	_, _ = peer.Write(packet(segment{seq: 1001, ack: ack, flags: data, ts: stale, ecr: serverTS, payload: []byte("FAKE-HELLO")}))
	_, _ = peer.Write(packet(segment{seq: 1001, ack: ack, flags: data, ts: 5002, ecr: serverTS, payload: []byte("REAL-HELLO")}))
	if got := readTarget(t, far, 10); got != "REAL-HELLO" {
		t.Fatalf("до сайта дошло %q вместо настоящего приветствия", got)
	}
}

func TestDecoysWithMD5OrALowTTLNeverReachTheSite(t *testing.T) {
	peer, ack, serverTS, far := connect(t, true)
	data := header.TCPFlagAck | header.TCPFlagPsh
	_, _ = peer.Write(packet(segment{seq: 1001, ack: ack, flags: data, ts: 5002, ecr: serverTS, md5: true, payload: []byte("FAKE-MD5..")}))
	_, _ = peer.Write(packet(segment{seq: 1001, ack: ack, flags: data, ts: 5002, ecr: serverTS, ttl: 3, payload: []byte("FAKE-TTL..")}))
	_, _ = peer.Write(packet(segment{seq: 1001, ack: ack, flags: data, ts: 5003, ecr: serverTS, payload: []byte("REAL-HELLO")}))
	if got := readTarget(t, far, 10); got != "REAL-HELLO" {
		t.Fatalf("до сайта дошло %q вместо настоящего приветствия", got)
	}
}

func TestAConnectionWithoutTimestampsIsNotFooledByOne(t *testing.T) {
	peer, ack, _, far := connect(t, false)
	data := header.TCPFlagAck | header.TCPFlagPsh
	_, _ = peer.Write(packet(segment{seq: 1001, ack: ack, flags: data, ts: 1, payload: []byte("FAKE-HELLO")}))
	_, _ = peer.Write(packet(segment{seq: 1001, ack: ack, flags: data, noTS: true, payload: []byte("REAL-HELLO")}))
	if got := readTarget(t, far, 10); got != "REAL-HELLO" {
		t.Fatalf("до сайта дошло %q вместо настоящего приветствия", got)
	}
}

func TestZapretSequenceOverlapLeavesOnlyTheRealBytes(t *testing.T) {
	// --dpi-desync-split-seqovl=681: первый кусок начинается на 681 байт
	// раньше потока и несёт мусор; сервер отрезает всё, что левее начала.
	peer, ack, serverTS, far := connect(t, true)
	data := header.TCPFlagAck | header.TCPFlagPsh
	overlap := append(make([]byte, 681), 'R')
	_, _ = peer.Write(packet(segment{seq: 1001 - 681, ack: ack, flags: data, ts: 5002, ecr: serverTS, payload: overlap}))
	_, _ = peer.Write(packet(segment{seq: 1002, ack: ack, flags: data, ts: 5002, ecr: serverTS, payload: []byte("EAL-HELLO")}))
	if got := readTarget(t, far, 10); got != "REAL-HELLO" {
		t.Fatalf("до сайта дошло %q вместо настоящего приветствия", got)
	}
}
