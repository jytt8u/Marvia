package inbound_test

import (
	"bytes"
	"testing"

	"github.com/jytt8u/marvia/internal/inbound"
)

// Нода слушает открытый интернет, и первые байты любого соединения читает
// именно этот разбор: их присылает кто угодно, в том числе сканер цензора.
// Обещание — никакие байты не роняют ноду и не заставляют её читать
// больше, чем пришло.

func FuzzReadVLESSRequest(f *testing.F) {
	uuid := bytes.Repeat([]byte{0x11}, 16)
	f.Add(append(append([]byte{0}, uuid...), 0, 1, 0x01, 0xBB, 1, 1, 1, 1, 1))
	f.Add(append(append([]byte{0}, uuid...), 0, 1, 0x01, 0xBB, 2, 11, 'e', 'x', 'a', 'm', 'p', 'l', 'e', '.', 'c', 'o', 'm'))
	f.Add([]byte{0})
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		req, err := inbound.ReadVLESSRequest(bytes.NewReader(data))
		if err == nil && req == nil {
			t.Fatal("нет ни запроса, ни ошибки")
		}
		if err == nil && len(req.Target.Host) > 255 {
			t.Fatalf("имя цели длиннее 255: %d", len(req.Target.Host))
		}
	})
}

func FuzzReadTrojanRequest(f *testing.F) {
	pass := bytes.Repeat([]byte{'a'}, 56)
	f.Add(append(append(pass, '\r', '\n', 1, 3, 11), []byte("example.com\x01\xbb\r\n")...))
	f.Add(append(append(pass, '\r', '\n', 1, 1, 1, 1, 1, 1), 0x01, 0xBB, '\r', '\n'))
	f.Add([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		req, err := inbound.ReadTrojanRequest(bytes.NewReader(data))
		if err == nil && req == nil {
			t.Fatal("нет ни запроса, ни ошибки")
		}
	})
}
