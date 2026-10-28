//go:build windows

package main

import (
	"testing"

	"github.com/jytt8u/marvia/internal/client"
)

// TestRouteSaysWhoChoseTheFingerprint: в «Качестве связи» отпечаток —
// продавца, пока человек не выбрал свой; выбранный показывается с пометкой,
// что выбрал он, — иначе по снимку окна продавец не понял бы, откуда firefox.
func TestRouteSaysWhoChoseTheFingerprint(t *testing.T) {
	n := client.Node{Address: "203.0.113.1:443", SNI: "example.com", Fingerprint: "safari"}
	if r := vp1Route(n, true, ""); r.Fingerprint != "safari" || r.Chosen {
		t.Fatalf("без выбора: %+v", r)
	}
	if r := vp1Route(n, true, "firefox"); r.Fingerprint != "firefox" || !r.Chosen {
		t.Fatalf("выбран firefox: %+v", r)
	}
}
