package panel_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/panel"
	"github.com/jytt8u/marvia/internal/updater"
)

// Нода на тестовом выпуске должна уметь дойти до следующего: alpha.5 и
// alpha.6 — разные версии, а не одна «0.13.0». И тестовый выпуск всегда
// раньше стабильного с тем же номером.
func TestAnAlphaIsOlderThanTheNextAlphaAndTheRelease(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		older bool
	}{
		{"v0.13.0-alpha.5", "v0.13.0-alpha.6", true},
		{"v0.13.0-alpha.9", "v0.13.0-alpha.10", true},
		{"v0.13.0-alpha.6", "v0.13.0-beta.1", true},
		{"v0.13.0-beta.2", "v0.13.0-rc.1", true},
		{"v0.13.0-rc.1", "v0.13.0", true},
		{"v0.12.0", "v0.13.0-alpha.5", true},
		{"v0.13.0-alpha.6", "v0.13.0-alpha.5", false},
		{"v0.13.0", "v0.13.0-rc.1", false},
		{"v0.13.0-alpha.5", "v0.12.2", false},
		{"v0.13.0-alpha.5", "v0.13.0-alpha.5", false},
		{"dev", "v0.13.0-alpha.5", false},
	} {
		if got := panel.OlderVersion(c.a, c.b); got != c.older {
			t.Errorf("%s старше %s: %v, ждали %v", c.a, c.b, got, c.older)
		}
	}
}

// fakeReleases — GitHub с лентой меток и файлами только у опубликованных:
// у черновика SHA256SUMS снаружи не отдаётся.
func fakeReleases(t *testing.T, stable string, feed []string, drafts ...string) {
	t.Helper()
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/releases/latest":
			http.Redirect(w, r, "https://github.com/jytt8u/marvia/releases/tag/"+stable, http.StatusFound)
		case r.URL.Path == "/releases.atom":
			for _, tag := range feed {
				fmt.Fprintf(w, "<entry><link href=\"https://github.com/jytt8u/marvia/releases/tag/%s\"/></entry>\n", tag)
			}
		case strings.HasPrefix(r.URL.Path, "/releases/download/"):
			tag := strings.Split(strings.TrimPrefix(r.URL.Path, "/releases/download/"), "/")[0]
			for _, d := range drafts {
				if d == tag {
					http.NotFound(w, r)
					return
				}
			}
			http.Redirect(w, r, "https://objects.example/"+tag, http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(gh.Close)
	t.Cleanup(panel.SetReleaseURL(gh.URL + "/releases/latest"))
}

// withChannel ставит канал так, как его ставит root на сервере, — файлом в
// каталоге службы обновления.
func withChannel(t *testing.T, channel string) {
	t.Helper()
	dir := t.TempDir()
	if channel != "" {
		if err := os.WriteFile(filepath.Join(dir, "channel"), []byte(channel+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	was := updater.StateDir
	updater.StateDir = dir
	t.Cleanup(func() { updater.StateDir = was })
}

// Сервер на тестовом канале видит в панели самый новый опубликованный
// выпуск — не черновик и не правку старой ветки, — и по нему же просит
// обновиться ноды.
func TestTestChannelShowsTheNewestPublishedRelease(t *testing.T) {
	withChannel(t, "test")
	fakeReleases(t, "v0.12.3",
		[]string{"v0.12.3", "v1.0.1", "v0.13.0-alpha.6", "v0.13.0-alpha.5", "test-0.12.3", "v0.12.2"},
		"v1.0.1")
	h := newHarness(t)
	node := h.createNode("alpha")
	h.do(http.MethodPost, "/api/v1/node/usage", node.Token, map[string]any{"usage": map[string]any{}, "version": "v0.13.0-alpha.5"}, nil)

	var view struct {
		Latest  string `json:"latest"`
		Channel string `json:"channel"`
	}
	h.do(http.MethodGet, "/api/v1/updates", adminToken, nil, &view)
	if view.Latest != "v0.13.0-alpha.6" || view.Channel != "test" {
		t.Fatalf("тестовый канал показывает %+v", view)
	}
	var asked struct {
		Asked  int    `json:"asked"`
		Target string `json:"target"`
	}
	h.do(http.MethodPost, "/api/v1/updates/nodes", adminToken, nil, &asked)
	if asked.Asked != 1 || asked.Target != "v0.13.0-alpha.6" {
		t.Fatalf("ноду на alpha.5 не попросили дойти до alpha.6: %+v", asked)
	}
}

// Без выбора root панель показывает стабильный релиз и говорит, что канал
// стабильный: тестовые сборки продавцу сами не предлагаются.
func TestWithoutAChoiceThePanelOffersOnlyStableReleases(t *testing.T) {
	withChannel(t, "")
	fakeReleases(t, "v0.12.2", []string{"v0.13.0-alpha.6", "v0.12.2"})
	h := newHarness(t)
	var view struct {
		Latest  string `json:"latest"`
		Channel string `json:"channel"`
	}
	h.do(http.MethodGet, "/api/v1/updates", adminToken, nil, &view)
	if view.Latest != "v0.12.2" || view.Channel != "stable" {
		t.Fatalf("без выбора канала: %+v", view)
	}
}
