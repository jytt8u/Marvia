package client

import (
	"testing"

	utls "github.com/refraction-networking/utls"
)

// TestChosenFingerprintBeatsTheSellersOnlyWhenChosen: выбранный человеком
// отпечаток заменяет продавцов; не выбранный — остаётся продавцов; незнакомое
// имя — Chrome, чтобы подключиться, а не упасть.
func TestChosenFingerprintBeatsTheSellersOnlyWhenChosen(t *testing.T) {
	node := Node{Fingerprint: "safari"}
	if got := fingerprintFor(node, Options{}); got != utls.HelloSafari_Auto {
		t.Fatalf("без выбора: %v", got)
	}
	if got := fingerprintFor(node, Options{Fingerprint: "firefox"}); got != utls.HelloFirefox_Auto {
		t.Fatalf("выбран firefox: %v", got)
	}
	// Нулевое значение транспорт читает как Chrome (transport.Fingerprint).
	if got := fingerprintFor(Node{Fingerprint: "незнакомый"}, Options{}); got != (utls.ClientHelloID{}) {
		t.Fatalf("незнакомый у продавца: %v", got)
	}
}
