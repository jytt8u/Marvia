package mobile

import (
	"github.com/jytt8u/marvia/internal/client"
	"testing"
)

type updateBackend struct {
	client.Backend
	sub client.Subscription
}

func (b updateBackend) Subscription() client.Subscription { return b.sub }

func TestAndroidReceivesTheNewerTestReleaseAndItsDownloadLink(t *testing.T) {
	const url = "https://panel.example.test/sub/test/app/android"
	tunnel := &Tunnel{dialer: updateBackend{sub: client.Subscription{
		Apps: map[string]client.AppOffer{"android": {Version: "0.13.0-alpha.6", URL: url}},
	}}}
	if got := tunnel.UpdateVersion("0.13.0-alpha.5"); got != "0.13.0-alpha.6" {
		t.Fatalf("экран не получил alpha.6: %q", got)
	}
	if got := tunnel.UpdateURL("0.13.0-alpha.5"); got != url {
		t.Fatalf("экран не получил ссылку: %q", got)
	}
	for _, current := range []string{"0.13.0-alpha.6", "0.13.0-beta.1", "0.13.0", "dev"} {
		if tunnel.UpdateVersion(current) != "" || tunnel.UpdateURL(current) != "" {
			t.Errorf("%s получил предложение отката или повторной установки", current)
		}
	}
}
