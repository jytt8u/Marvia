//go:build windows

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestOneHangingCheckPageDoesNotFailTheCheck: проверочная страница, которая
// висит, не съедает срок остальных — ответила любая, значит путь живой.
// Раньше страницы шли по очереди, и висящая первая давала «не открывается и
// через туннель», хотя сайты открывались.
func TestOneHangingCheckPageDoesNotFailTheCheck(t *testing.T) {
	stop := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-stop:
		case <-r.Context().Done():
		}
	}))
	defer hang.Close()
	defer close(stop)
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ok.Close()

	was := probeURLs
	probeURLs = []string{hang.URL, ok.URL}
	defer func() { probeURLs = was }()

	start := time.Now()
	if err := probeWith(context.Background(), &http.Transport{Proxy: nil}); err != nil {
		t.Fatalf("живая страница не засчитана: %v", err)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("проверка ждала висящую страницу %v", took)
	}
}

// TestPortalOnPlainHTTPIsNotTheInternet: гостиничный портал отвечает на
// открытый HTTP своей страницей (200) — это не интернет, и проверка его так
// не засчитывает.
func TestPortalOnPlainHTTPIsNotTheInternet(t *testing.T) {
	portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>войдите в сеть отеля</html>"))
	}))
	defer portal.Close()
	was := probeURLs
	probeURLs = []string{portal.URL}
	defer func() { probeURLs = was }()

	if err := probeWith(context.Background(), &http.Transport{Proxy: nil}); err == nil {
		t.Fatal("страница портала засчитана за интернет")
	}
}
