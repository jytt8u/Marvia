package version_test

import (
	"github.com/jytt8u/marvia/internal/version"
	"testing"
)

func TestReleaseOrderIsSharedByClientsAndPanel(t *testing.T) {
	for _, tc := range []struct {
		a, b  string
		older bool
	}{
		{"v0.13.0-alpha.5", "0.13.0-alpha.6", true},
		{"0.13.0-alpha.9", "0.13.0-alpha.10", true},
		{"0.13.0-alpha.6", "0.13.0-beta.1", true},
		{"0.13.0-beta.2", "0.13.0-rc.1", true},
		{"0.13.0-rc.1", "0.13.0", true},
		{"0.12.2", "0.13.0-alpha.1", true},
		{"0.13.0-alpha.1", "0.12.2", false},
		{"0.13.0-alpha.1", "0.13.0-alpha.1.1", true},
		{"0.13.0-1", "0.13.0-alpha", true},
		{"0.13.0-alpha.6+build.1", "0.13.0-alpha.6+build.2", false},
		{"0.13.0-alpha.5+build.99", "0.13.0-alpha.6+build.1", true},
		{"  v0.13.0-alpha.5  ", "0.13.0-alpha.6", true},
		{"0.13", "v0.13.0", false},
		{"0.12", "v0.13.0-alpha.6", true},
		{"dev", "0.13.0-alpha.6", false},
		{"0.13.0", "dev", false},
		{"", "0.13.0", false},
		{"0.13.0-", "0.13.0", false},
		{"0.13.bad", "0.13.0", false},
		{"0.13.0.1", "0.13.0", false},
	} {
		t.Run(tc.a+"→"+tc.b, func(t *testing.T) {
			if got := version.Older(tc.a, tc.b); got != tc.older {
				t.Errorf("Older: %v, ждали %v", got, tc.older)
			}
			if got := version.Newer(tc.b, tc.a); got != tc.older {
				t.Errorf("Newer: %v, ждали %v", got, tc.older)
			}
		})
	}
}
