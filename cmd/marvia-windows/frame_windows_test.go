//go:build windows

package main

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Каждая кнопка окна, которую рисует страница, и каждая просьба её скрипта
// есть у окна. Системного заголовка больше нет: кнопка без действия здесь
// оставила бы окно, которое нельзя свернуть или закрыть.
func TestEveryWindowButtonOnThePageHasAnAction(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	buttons := regexp.MustCompile(`data-win="([a-z]+)"`).FindAllStringSubmatch(page, -1)
	if len(buttons) < 3 {
		t.Fatalf("кнопок окна на странице %d, а нужно свернуть, развернуть и закрыть", len(buttons))
	}
	for _, b := range buttons {
		if !slices.Contains(windowActions, b[1]) {
			t.Errorf("кнопка %q нарисована, а окно её не знает", b[1])
		}
	}
	for _, a := range []string{"drag", "top", "max", "state", "through", "solid"} {
		if !strings.Contains(page, `winAct("`+a+`")`) {
			t.Errorf("страница не просит окно о %q — перетаскивание или разворот потерялись", a)
		}
		if !slices.Contains(windowActions, a) {
			t.Errorf("окно не умеет %q", a)
		}
	}
}
