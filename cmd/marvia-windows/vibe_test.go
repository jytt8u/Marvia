package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Каждый образ из таблицы VIBES подписан на обоих языках. Без подписи
// карточка показала бы пустое место, а код упал бы на d.vibes[id][0] — и
// вместе с ним вся вкладка «Тема».
func TestEveryVibeIsNamedInBothLanguages(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	start := strings.Index(page, "const VIBES = {")
	if start < 0 {
		t.Fatal("в странице нет таблицы образов")
	}
	end := strings.Index(page[start:], "\n};")
	ids := regexp.MustCompile(`(?m)^  ([a-z]+): \{$`).FindAllStringSubmatch(page[start:start+end], -1)
	if len(ids) < 4 {
		t.Fatalf("образов подозрительно мало: %d", len(ids))
	}
	dicts := regexp.MustCompile(`(?s)    vibes: \{\n(.*?)\n    \},`).FindAllStringSubmatch(page, -1)
	if len(dicts) != 2 {
		t.Fatalf("подписи образов должны быть в двух словарях, нашлось %d", len(dicts))
	}
	for _, id := range ids {
		for i, d := range dicts {
			if !strings.Contains(d[1], "      "+id[1]+": [") {
				t.Errorf("образ %q не подписан в словаре %d", id[1], i+1)
			}
		}
	}
}

// Заставка закрывает окно целиком. Если код страницы до неё не дойдёт —
// ошибка в скрипте, медленный WebView, — она обязана уйти сама, иначе
// человек увидит вместо VPN красивую, но вечную картинку.
func TestSplashLeavesEvenWithoutScript(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)
	rule := regexp.MustCompile(`#splash \{[^}]*animation: splash-failsafe 0s ([0-9.]+)s forwards`).FindStringSubmatch(page)
	if rule == nil {
		t.Fatal("у заставки нет запасного ухода без скрипта")
	}
	if !regexp.MustCompile(`@keyframes splash-failsafe \{ to \{[^}]*visibility: hidden`).MatchString(page) {
		t.Fatal("запасной уход заставки не прячет её")
	}
}
