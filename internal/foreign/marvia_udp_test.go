package foreign

import (
	"context"
	"io"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/inbound"
	"github.com/jytt8u/marvia/internal/relay"
	"github.com/jytt8u/marvia/internal/vp1"
)

// marviaVLESS — VLESS так, как его разбирает нода Marvia: тем же разбором
// запроса и теми же обработчиками UDP. Отличие одно — исходящий UDP идёт
// прямо, без запретов адресов: цель в тесте на 127.0.0.1.
func marviaVLESS(t *testing.T) string {
	t.Helper()
	const uuid = "b831381d-6324-4d53-ad4f-8cda48b30811"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	dial := func(to vp1.Address) (net.Conn, error) { return net.Dial("udp", to.String()) }
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				req, err := inbound.ReadVLESSRequest(c)
				if err != nil {
					return
				}
				client := inbound.NewVLESSConn(c)
				switch {
				case req.XUDP:
					_ = inbound.ServeXUDP(client, dial, 5*time.Second)
				case req.UDP:
					target, err := dial(req.Target)
					if err != nil {
						return
					}
					relay.Datagrams(vp1.Datagrams(client), target, 5*time.Second)
				default:
					_, _ = io.Copy(io.Discard, c)
				}
			}()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	return "vless://" + uuid + "@127.0.0.1:" + strconv.Itoa(port) + "?security=none#Marvia"
}

// udpEcho — сервер, отвечающий «эхо:» на каждую датаграмму.
func udpEcho(t *testing.T, addr string) vp1.Address {
	t.Helper()
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		t.Skipf("порт %s занят: %v", addr, err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(append([]byte("эхо:"), buf[:n]...), from)
		}
	}()
	a, _ := vp1.AddressFromHostPort("127.0.0.1", uint16(pc.LocalAddr().(*net.UDPAddr).Port))
	return a
}

func roundTrip(t *testing.T, link string, target vp1.Address) {
	t.Helper()
	l, err := Parse(link)
	if err != nil {
		t.Fatal(err)
	}
	e, err := Start(l)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := e.DialDatagrams(ctx, target)
	if err != nil {
		t.Fatalf("дозвон: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	for _, msg := range []string{"раз", "два", "три"} {
		if _, err := conn.Write([]byte(msg)); err != nil {
			t.Fatalf("запись: %v", err)
		}
		buf := make([]byte, 2048)
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatalf("ответ на %q не пришёл: %v", msg, err)
		}
		if got := string(buf[:n]); got != "эхо:"+msg {
			t.Fatalf("ответ %q, ожидался %q", got, "эхо:"+msg)
		}
	}
}

// TestGamesGoThroughAMarviaNodeOverVLESS: UDP на обычный игровой порт Xray
// шлёт через XUDP. Нода Marvia отвечала на это «mux.cool не
// поддерживается», и игры по ссылке vless:// не грузились совсем.
func TestGamesGoThroughAMarviaNodeOverVLESS(t *testing.T) {
	roundTrip(t, marviaVLESS(t), udpEcho(t, "127.0.0.1:0"))
}

// TestQUICGoesThroughAMarviaNodeOverVLESS: на порт 443 Xray шлёт UDP простой
// командой, без XUDP. Нода отвечала «UDP пока не поддерживается».
func TestQUICGoesThroughAMarviaNodeOverVLESS(t *testing.T) {
	roundTrip(t, marviaVLESS(t), udpEcho(t, "127.0.0.1:443"))
}

// marviaTrojan — Trojan так, как его разбирает нода Marvia.
func marviaTrojan(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	dial := func(to vp1.Address) (net.Conn, error) { return net.Dial("udp", to.String()) }
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				req, err := inbound.ReadTrojanRequest(c)
				if err != nil || !req.UDP {
					return
				}
				_ = inbound.ServeTrojanUDP(c, dial, 5*time.Second)
			}()
		}
	}()
	return "trojan://secret@127.0.0.1:" + strconv.Itoa(ln.Addr().(*net.TCPAddr).Port) + "?security=none#Marvia"
}

// TestGamesGoThroughAMarviaNodeOverTrojan: у Trojan UDP — пакеты с адресом
// в каждом; нода отвечала на них «UDP пока не поддерживается».
func TestGamesGoThroughAMarviaNodeOverTrojan(t *testing.T) {
	roundTrip(t, marviaTrojan(t), udpEcho(t, "127.0.0.1:0"))
}
