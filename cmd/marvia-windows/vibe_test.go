package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/jytt8u/marvia/internal/look"
)

// Каждый образ из таблицы VIBES подписан на обоих языках. Без подписи
// карточка показала бы пустое место, а код упал бы на d.vibes[id][0] — и
// вместе с ним вся вкладка «Тема».
func TestEveryVibeIsNamedInBothLanguages(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	// На Windows git отдаёт файл с CRLF, а шаблоны ниже — построчные.
	page := strings.ReplaceAll(string(raw), "\r\n", "\n")
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

// Скрипт окна вообще разбирается. Одна синтаксическая ошибка в нём — и окно
// показывает голую разметку: ни перевода, ни кнопки, ни опроса состояния.
// Сборка Go такого не видит, поэтому проверяем тем же разбором, что у
// браузера, — через node, если он есть на машине.
func TestWindowScriptParses(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("нет node — проверить разбор скрипта нечем")
	}
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	page := look.Inline(string(raw))
	start, end := strings.Index(page, "<script>"), strings.LastIndex(page, "</script>")
	if start < 0 || end < start {
		t.Fatal("в странице нет скрипта")
	}
	file := filepath.Join(t.TempDir(), "app.js")
	if err := os.WriteFile(file, []byte(page[start+len("<script>"):end]), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(node, "--check", file).CombinedOutput(); err != nil {
		t.Fatalf("скрипт окна не разбирается:\n%s", out)
	}
}
