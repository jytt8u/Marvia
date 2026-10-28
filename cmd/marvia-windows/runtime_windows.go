//go:build windows

package main

import (
	"errors"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	webViewRuntimeKey  = `Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}`
	webViewDownloadURL = "https://developer.microsoft.com/microsoft-edge/webview2/#download-section"
)

var (
	errWebViewRuntimeMissing = errors.New("WebView2 Runtime не найден или недоступен")
	errWebViewWindow         = errors.New("окно WebView2 не создано")
)

// Проверяем до запроса UAC и автоподключения. У оконного EXE нет Ctrl+C:
// запасной запуск браузера оставлял процесс без трея и понятного выхода.
// Две ветки и pv определены Microsoft, наличие обычного Edge недостаточно.
func checkWebViewRuntime(read func(registry.Key, string, uint32) (string, error)) error {
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		access := uint32(registry.QUERY_VALUE)
		if root == registry.LOCAL_MACHINE {
			access |= registry.WOW64_32KEY
		}
		version, err := read(root, webViewRuntimeKey, access)
		if err != nil {
			continue
		}
		parts := strings.Split(version, ".")
		if len(parts) != 4 {
			continue
		}
		valid, positive := true, false
		for _, part := range parts {
			n, err := strconv.ParseUint(part, 10, 32)
			if err != nil {
				valid = false
				break
			}
			positive = positive || n != 0
		}
		if valid && positive {
			return nil
		}
	}
	return errWebViewRuntimeMissing
}

func readRuntimeVersion(root registry.Key, path string, access uint32) (string, error) {
	key, err := registry.OpenKey(root, path, access)
	if err != nil {
		return "", err
	}
	defer key.Close()
	version, _, err := key.GetStringValue("pv")
	return version, err
}
