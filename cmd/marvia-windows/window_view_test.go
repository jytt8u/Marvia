//go:build windows

package main

import (
	"strings"
	"testing"

	"github.com/jchv/go-webview2"
)

type viewRecorder struct {
	webview2.WebView
	navigation, scripts []string
}

func (v *viewRecorder) Navigate(page string) { v.navigation = append(v.navigation, page) }
func (v *viewRecorder) Eval(script string)   { v.scripts = append(v.scripts, script) }

func TestSwitchingTrayKeepsTheSamePage(t *testing.T) {
	view := &viewRecorder{}
	s := &shell{w: view, url: "http://127.0.0.1/test/"}
	s.navigateView("full", "home")
	s.navigateView("widget", "")
	s.navigateView("full", "settings")
	if len(view.navigation) != 1 || len(view.scripts) != 2 {
		t.Fatal("переключение трея перезагрузило страницу и потеряло её состояние")
	}
	if !strings.Contains(view.scripts[1], `"tab":"settings"`) {
		t.Fatal("потеряна выбранная вкладка")
	}
}
