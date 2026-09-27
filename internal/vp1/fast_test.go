package vp1

import (
	"bytes"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func fastPair(t *testing.T, allow Authorizer) (*Conn, *Conn, []byte) {
	t.Helper()
	return handshakePairMode(t, allow, true)
}

func TestFastTransfersBothDirections(t *testing.T) {
	client, server, _ := fastPair(t, AllowAll)
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := server.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, direction := range []struct{ from, to *Conn }{{client, server}, {server, client}} {
		payload := bytes.Repeat([]byte("VP1 Fast — проверенный поток"), 4000)
		done := make(chan error, 1)
		go func() { _, err := direction.from.Write(payload); done <- err }()
		got := make([]byte, len(payload))
		if _, err := io.ReadFull(direction.to, got); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("полезные данные изменились")
		}
	}
}

func TestFastRejectsRecordAttacks(t *testing.T) { securityTransportAttacks(t, fastPair) }

// Одни ключи не делают протоколы взаимозаменяемыми: никаких автоматических
// понижений защиты или попыток повторить вход другим шифром.
func TestFastAndVP1CannotBeConfused(t *testing.T) {
	for _, fastClient := range []bool{false, true} {
		t.Run(map[bool]string{false: "vp1-to-fast", true: "fast-to-vp1"}[fastClient], func(t *testing.T) {
			key, err := GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			clientKey, err := GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			clientHandshake, serverHandshake := ClientHandshake, ServerFastHandshake
			if fastClient {
				clientHandshake, serverHandshake = ClientFastHandshake, ServerHandshake
			}
			done := make(chan error, 1)
			go func() {
				defer b.Close()
				_, _, err := serverHandshake(b, key, NewReplayGuard(ClockSkew), AllowAll)
				done <- err
			}()
			_, clientErr := clientHandshake(a, clientKey, key.Public)
			if serverErr := <-done; clientErr == nil || serverErr == nil {
				t.Fatalf("смешение протоколов принято: %v / %v", clientErr, serverErr)
			}
		})
	}
}

func TestFastRequiresAuthorizationAndReplayProtection(t *testing.T) {
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	for _, absentGuard := range []bool{false, true} {
		guard, allow := NewReplayGuard(ClockSkew), Authorizer(AllowAll)
		if absentGuard {
			guard = nil
		} else {
			allow = nil
		}
		if _, _, err := ServerFastHandshake(nil, key, guard, allow); err == nil {
			t.Fatal("отсутствующая защита не отклонена до чтения сети")
		}
	}
}

func TestFastRejectsReplayExpiredAndUnauthorizedHandshakes(t *testing.T) {
	key, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	attempt := func(data []byte, guard *ReplayGuard, allow Authorizer) (*attackWire, error) {
		a, b := net.Pipe()
		defer b.Close()
		defer a.Close()
		wire := &attackWire{Conn: a, input: bytes.NewReader(data)}
		_, _, err := ServerFastHandshake(wire, key, guard, allow)
		return wire, err
	}
	msg := securityFirstMessageWith(t, key, time.Now(), fastCipherSuite, FastPrologue)
	guard := NewReplayGuard(ClockSkew)
	if _, err := attempt(msg, guard, AllowAll); err != nil {
		t.Fatal(err)
	}
	if wire, err := attempt(msg, guard, AllowAll); !errors.Is(err, ErrReplay) || wire.output.Len() != 0 {
		t.Fatalf("повтор: %v", err)
	}
	for _, offset := range []time.Duration{-3 * ClockSkew, 3 * ClockSkew} {
		stale := securityFirstMessageWith(t, key, time.Now().Add(offset), fastCipherSuite, FastPrologue)
		if wire, err := attempt(stale, NewReplayGuard(ClockSkew), AllowAll); !errors.Is(err, ErrClockSkew) || wire.output.Len() != 0 {
			t.Fatalf("время: %v", err)
		}
	}
	deniedGuard := NewReplayGuard(ClockSkew)
	if wire, err := attempt(msg, deniedGuard, func([]byte) error { return ErrUnauthorized }); !errors.Is(err, ErrUnauthorized) || wire.output.Len() != 0 || deniedGuard.Size() != 0 {
		t.Fatalf("чужой ключ: %v", err)
	}
}

func TestFastRejectsForgedClientKeyAndWrongNode(t *testing.T) {
	for _, kind := range []string{"wrong-node", "forged-client"} {
		t.Run(kind, func(t *testing.T) {
			node, err := GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			client, err := GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			other, err := GenerateKeyPair()
			if err != nil {
				t.Fatal(err)
			}
			allowed := bytes.Clone(client.Public)
			serverPub := node.Public
			if kind == "wrong-node" {
				serverPub = other.Public
			} else {
				client.Private = other.Private
			}
			a, b := net.Pipe()
			defer a.Close()
			defer b.Close()
			done := make(chan error, 1)
			go func() {
				defer b.Close()
				_, _, err := ServerFastHandshake(b, node, NewReplayGuard(ClockSkew), func(pub []byte) error {
					if !bytes.Equal(pub, allowed) {
						return ErrUnauthorized
					}
					return nil
				})
				done <- err
			}()
			_, clientErr := ClientFastHandshake(a, client, serverPub)
			if serverErr := <-done; clientErr == nil || serverErr == nil {
				t.Fatalf("подмена принята: %v / %v", clientErr, serverErr)
			}
		})
	}
}
