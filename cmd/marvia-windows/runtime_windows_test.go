//go:build windows

package main

import (
	"errors"
	"testing"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows/registry"
)

func TestRuntimeCheckUsesBothOfficialInstallLocations(t *testing.T) {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		err := checkWebViewRuntime(func(key registry.Key, _ string, access uint32) (string, error) {
			if key == root {
				if key == registry.LOCAL_MACHINE && access&registry.WOW64_32KEY == 0 {
					t.Fatal("проверяется 64-битная ветка вместо официальной ветки EdgeUpdate")
				}
				return "140.0.3485.81", nil
			}
			return "", registry.ErrNotExist
		})
		if err != nil {
			t.Fatalf("установленный Runtime не найден: %v", err)
		}
	}
}

func TestEmptyOrBrokenRuntimeRegistrationDoesNotStartTheApp(t *testing.T) {
	for _, version := range []string{"", "0.0.0.0", "140", "140.0.bad.1", "140.0.0.0x", "-1.0.0.0", "140.0.0.0.1"} {
		t.Run(version, func(t *testing.T) {
			err := checkWebViewRuntime(func(registry.Key, string, uint32) (string, error) {
				return version, nil
			})
			if !errors.Is(err, errWebViewRuntimeMissing) {
				t.Fatalf("неработающий Runtime ошибочно признан установленным: %q", version)
			}
		})
	}
	if err := checkWebViewRuntime(func(registry.Key, string, uint32) (string, error) {
		return "140.0.0.1", registry.ErrNotExist
	}); !errors.Is(err, errWebViewRuntimeMissing) {
		t.Fatal("ошибка чтения реестра принята за работающий Runtime")
	}
}

func TestFailedWindowCreationReturnsWithoutBrowserOrBackgroundProcess(t *testing.T) {
	oldFactory := createWebView
	createWebView = func(webview2.WebViewOptions) webview2.WebView { return nil }
	t.Cleanup(func() { createWebView = oldFactory })
	var onWindow func(windowRequest)
	if err := showWindow("http://127.0.0.1/private-test/", nil, nil, false, &onWindow); !errors.Is(err, errWebViewWindow) {
		t.Fatalf("сбой окна не завершает запуск: %v", err)
	}
	if onWindow != nil {
		t.Fatal("у неработающего окна остался обработчик")
	}
}
