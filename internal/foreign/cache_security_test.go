package foreign

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestDifferentSubscriptionsDoNotShareKnownCollidingCacheNames(t *testing.T) {
	// У прежнего четырёхбайтового отпечатка эти два адреса совпадали.
	a := CachePath(t.TempDir(), "https://example.invalid/sub/21881")
	b := CachePath(filepath.Dir(a), "https://example.invalid/sub/24537")
	if a == b {
		t.Fatal("разные подписки получили один файл кэша")
	}
}

func TestForeignCacheDoesNotReturnAnotherSubscriptionsCredentials(t *testing.T) {
	for _, age := range []time.Duration{0, Fresh + time.Hour} {
		t.Run(age.String(), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/original" {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write([]byte("trojan://original-secret@example.test:443"))
			}))
			defer server.Close()
			path := filepath.Join(t.TempDir(), "subscription.json")
			if _, _, _, err := Load(server.URL+"/original", path, false); err != nil {
				t.Fatal(err)
			}
			if age != 0 {
				changeForeignCacheTime(t, path, time.Now().Add(-age))
			}
			// Подмена пути моделирует коллизию имени; проверять надо и тело.
			if sub, _, _, err := Load(server.URL+"/other", path, false); err == nil || len(sub.Links) != 0 {
				t.Fatal("чужая подписка получила сохранённые пароли исходной")
			}
		})
	}
}

func TestFutureDatedForeignCacheIsRefreshed(t *testing.T) {
	var count atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		_, _ = w.Write([]byte("trojan://secret@example.test:443"))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "subscription.json")
	if _, _, _, err := Load(server.URL, path, false); err != nil {
		t.Fatal(err)
	}
	changeForeignCacheTime(t, path, time.Now().Add(365*24*time.Hour))
	if _, _, _, err := Load(server.URL, path, false); err != nil {
		t.Fatal(err)
	}
	if count.Load() != 2 {
		t.Fatal("дата из будущего сделала кэш постоянно свежим")
	}
}

func TestBoundForeignCacheStillWorksWhenItsPanelIsUnavailable(t *testing.T) {
	for _, refresh := range []bool{false, true} {
		t.Run(map[bool]string{false: "свежий", true: "устаревший"}[refresh], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte("trojan://secret@example.test:443"))
			}))
			path := CachePath(t.TempDir(), server.URL)
			if _, _, _, err := Load(server.URL, path, false); err != nil {
				server.Close()
				t.Fatal(err)
			}
			server.Close()
			sub, _, stale, err := Load(server.URL, path, refresh)
			if err != nil || len(sub.Links) != 1 || stale != refresh {
				t.Fatalf("собственный кэш не доступен без панели: нод=%d, устарел=%v, ошибка=%v", len(sub.Links), stale, err)
			}
		})
	}
}

func TestForgettingForeignSubscriptionRemovesCurrentAndLegacySecrets(t *testing.T) {
	dir := t.TempDir()
	const link = "https://example.invalid/sub/21881"
	paths := []string{CachePath(dir, link), filepath.Join(dir, "foreign-23fdc4fe.json")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("секрет"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ForgetCache(dir, link)
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("кэш удалённой подписки остался: %v", err)
		}
	}
}

func TestCachedRejectsUnboundAndMismatchedSubscriptions(t *testing.T) {
	const link = "https://example.invalid/sub/current"
	for _, fingerprint := range []string{"", subscriptionFingerprint(link + "/other"), subscriptionFingerprint(link)} {
		t.Run(fingerprint, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "subscription.json")
			raw, err := json.Marshal(cacheFile{
				Subscription: fingerprint,
				FetchedAt:    time.Now().Unix(),
				Body:         "trojan://secret@example.test:443",
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			sub, ok := Cached(link, path)
			if fingerprint == subscriptionFingerprint(link) {
				if !ok || len(sub.Links) != 1 {
					t.Fatal("собственный кэш не возвращён")
				}
			} else if ok || len(sub.Links) != 0 {
				t.Fatal("интерфейс получил непривязанный или чужой кэш")
			}
		})
	}
}

func changeForeignCacheTime(t *testing.T, path string, date time.Time) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	stored["fetched_at"] = date.Unix()
	raw, err = json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}
