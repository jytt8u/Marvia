package vp1

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
)

// После оборванного кадра прежний nonce уже нельзя продолжать вслепую.
// Транспорт оставляет буфер доступным даже после Close, как это бывает
// у обёрток с уже прочитанными данными: проверяем именно обещание VP1.
func TestSecurityBrokenRecordCannotBeRetried(t *testing.T) {
	for _, kind := range []string{"нулевая длина", "слишком длинный кадр", "подменённый тег"} {
		t.Run(kind, func(t *testing.T) {
			client, server, _ := handshakePair(t, AllowAll)
			capture := &capturedWrites{Conn: client.Conn}
			client.Conn = capture
			if _, err := client.Write([]byte("секрет после ошибки")); err != nil {
				t.Fatal(err)
			}
			good := capture.frames[0]
			bad := []byte{0, 0}
			switch kind {
			case "слишком длинный кадр":
				bad = []byte{255, 255}
			case "подменённый тег":
				bad = bytes.Clone(good)
				bad[len(bad)-1] ^= 1
			}
			wire := &attackWire{Conn: server.Conn, input: bytes.NewReader(append(bad, good...))}
			server.Conn = wire
			for attempt := 0; attempt < 2; attempt++ {
				if n, err := server.Read(make([]byte, 128)); n != 0 || err == nil || !wire.closed {
					t.Fatalf("чтение %d после повреждения: n=%d, ошибка=%v, закрыто=%v", attempt+1, n, err, wire.closed)
				}
			}
		})
	}
}

type brokenRecordWriter struct {
	net.Conn
	err    error
	writes int
	closed bool
}

func (w *brokenRecordWriter) Write(p []byte) (int, error) {
	w.writes++
	return len(p) / 2, w.err
}

func (w *brokenRecordWriter) Close() error { w.closed = true; return w.Conn.Close() }

func TestIncompleteEncryptedWriteClosesTheSession(t *testing.T) {
	for _, writeErr := range []error{nil, io.ErrUnexpectedEOF} {
		client, _, _ := handshakePair(t, AllowAll)
		wire := &brokenRecordWriter{Conn: client.Conn, err: writeErr}
		client.Conn = wire
		want := writeErr
		if want == nil {
			want = io.ErrShortWrite
		}
		for attempt := 0; attempt < 2; attempt++ {
			if n, err := client.Write([]byte("данные")); n != 0 || !errors.Is(err, want) || !wire.closed {
				t.Fatalf("запись %d: n=%d, ошибка=%v, закрыто=%v", attempt+1, n, err, wire.closed)
			}
		}
		if wire.writes != 1 {
			t.Fatal("повреждённая сессия отправила следующий кадр")
		}
	}
}

func TestTruncatedRecordClosesBothDirections(t *testing.T) {
	for _, truncated := range [][]byte{{0}, {0, 4}, {0, 4, 1, 2}} {
		_, server, _ := handshakePair(t, AllowAll)
		wire := &attackWire{Conn: server.Conn, input: bytes.NewReader(truncated)}
		server.Conn = wire
		if n, err := server.Read(make([]byte, 128)); n != 0 || !errors.Is(err, io.ErrUnexpectedEOF) || !wire.closed {
			t.Fatalf("оборванный кадр: n=%d, ошибка=%v, закрыто=%v", n, err, wire.closed)
		}
		if n, err := server.Write([]byte("ответ")); n != 0 || err == nil || wire.output.Len() != 0 {
			t.Fatal("оборванная сессия продолжила отправку")
		}
	}
}

func TestEndBetweenRecordsKeepsOutgoingHalfOpen(t *testing.T) {
	_, server, _ := handshakePair(t, AllowAll)
	wire := &attackWire{Conn: server.Conn, input: bytes.NewReader(nil)}
	server.Conn = wire
	if n, err := server.Read(make([]byte, 128)); n != 0 || !errors.Is(err, io.EOF) || wire.closed {
		t.Fatalf("чистое окончание входящего потока: n=%d, ошибка=%v, закрыто=%v", n, err, wire.closed)
	}
	if n, err := server.Write([]byte("ответ")); n == 0 || err != nil || wire.output.Len() == 0 {
		t.Fatal("чистое окончание чтения запретило ответ")
	}
}

// Перехватываем кадры до TLS, где наблюдатель (например, CDN) видит больше,
// чем оператор LTE. Отсутствие открытого текста и разброс длин проверяются
// отдельно от обещаний стойкости криптографии или неразличимости для DPI.
func TestSecurityCapturedRecordsHideContentAndVarySmallPacketSizes(t *testing.T) {
	client, server, clientPub := handshakePair(t, AllowAll)
	wire := &capturedWrites{Conn: client.Conn}
	client.Conn = wire
	payload := []byte("контрольный секрет: example.invalid")
	for i := 0; i < 100; i++ {
		if _, err := client.Write(payload); err != nil {
			t.Fatal(err)
		}
	}
	sizes := make(map[int]bool)
	minSize, maxSize := frameCap, 0
	for _, frame := range wire.frames {
		if bytes.Contains(frame, payload) || bytes.Contains(frame, clientPub) || bytes.Contains(frame, []byte(Prologue)) {
			t.Fatal("в перехваченном кадре есть открытое содержимое или ключ клиента")
		}
		plain, err := server.recv.Decrypt(nil, nil, frame[frameLenHeader:])
		if err != nil {
			t.Fatal(err)
		}
		got, err := unpackPadded(plain)
		if err != nil || !bytes.Equal(got, payload) {
			t.Fatal("расшифрованное содержимое изменилось")
		}
		sizes[len(frame)] = true
		minSize, maxSize = min(minSize, len(frame)), max(maxSize, len(frame))
	}
	if len(sizes) < 25 {
		t.Fatal("добивка снова оставляет почти постоянный размер мелкого кадра")
	}
	t.Logf("100 кадров: %d разных длин, диапазон %d…%d байт", len(sizes), minSize, maxSize)
}
