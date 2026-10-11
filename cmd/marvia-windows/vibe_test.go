package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
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

// У каждого образа с обоями они и правда нарисованы: имя без рисунка дало бы
// пустой фон вместо стола, и образ не узнать.
func TestEveryVibeWallpaperIsDrawn(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.ReplaceAll(string(raw), "\r\n", "\n")
	start := strings.Index(page, "const VIBES = {")
	end := strings.Index(page[start:], "\n};")
	walls := regexp.MustCompile(`wall: "([a-zA-Z]+)"`).FindAllStringSubmatch(page[start:start+end], -1)
	if len(walls) < 8 {
		t.Fatalf("образов с обоями подозрительно мало: %d", len(walls))
	}
	m := strings.Index(page, "const make = {")
	if m < 0 {
		t.Fatal("в странице нет рисовальщиков обоев")
	}
	drawers := page[m : m+strings.Index(page[m:], "\n  };")]
	for _, w := range walls {
		if !regexp.MustCompile(`\b` + w[1] + `\b`).MatchString(drawers) {
			t.Errorf("обои %q названы, но не нарисованы", w[1])
		}
	}
}

// Размытие на обоях не срезается прямоугольником. Область фильтра по
// умолчанию — рамка фигур плюс 10%, без толщины обводки и хвоста размытия, и
// всё за ней браузер обрезает ровно: у сияния arctic так появлялись прямые
// края поперёк неба. Поэтому область у каждого фильтра указана явно: в
// координатах листа — на весь лист, от рамки фигур — шире, чем по умолчанию.
// Обои строит скрипт, поэтому рисуем их им же через node, если он есть.
func TestWallpaperBlurIsNotCutToARectangle(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("нет node — нарисовать обои нечем")
	}
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	page := strings.ReplaceAll(string(raw), "\r\n", "\n")
	start := strings.Index(page, "const WALLS = (() => {")
	if start < 0 {
		t.Fatal("в странице нет рисовальщиков обоев")
	}
	end := start + strings.Index(page[start:], "\n})();") + len("\n})();")
	vibes := strings.Index(page, "const VIBES = {")
	var names []string
	for _, w := range regexp.MustCompile(`wall: "([a-zA-Z]+)"`).FindAllStringSubmatch(page[vibes:vibes+strings.Index(page[vibes:], "\n};")], -1) {
		names = append(names, w[1])
	}
	list, _ := json.Marshal(names)
	script := page[start:end] + `
const out = {};
for (const n of ` + string(list) + `) { const u = WALLS.url(n); out[n] = decodeURIComponent(u.slice(u.indexOf(",") + 1, -2)); }
process.stdout.write(JSON.stringify(out));
`
	file := filepath.Join(t.TempDir(), "walls.js")
	if err := os.WriteFile(file, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, file).Output()
	if err != nil {
		t.Fatalf("обои не рисуются: %v", err)
	}
	var walls map[string]string
	if err := json.Unmarshal(out, &walls); err != nil {
		t.Fatal(err)
	}

	num := func(s string) float64 {
		v, err := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 64)
		if err != nil {
			return 0
		}
		if strings.HasSuffix(s, "%") {
			v /= 100
		}
		return v
	}
	attr := regexp.MustCompile(`([a-zA-Z]+)="([^"]*)"`)
	filters := 0
	for name, svg := range walls {
		for _, tag := range regexp.MustCompile(`<filter\b[^>]*>`).FindAllString(svg, -1) {
			filters++
			a := map[string]string{}
			for _, m := range attr.FindAllStringSubmatch(tag, -1) {
				a[m[1]] = m[2]
			}
			if a["x"] == "" || a["y"] == "" || a["width"] == "" || a["height"] == "" {
				t.Errorf("обои %q: у размытия нет своей области, и браузер срежет его по рамке фигур: %s", name, tag)
				continue
			}
			x, y, w, h := num(a["x"]), num(a["y"]), num(a["width"]), num(a["height"])
			if a["filterUnits"] == "userSpaceOnUse" {
				if x > 0 || y > 0 || x+w < 1600 || y+h < 1000 {
					t.Errorf("обои %q: область размытия не покрывает лист 1600×1000: %s", name, tag)
				}
			} else if x >= -0.1 || y >= -0.1 || x+w <= 1.1 || y+h <= 1.1 {
				t.Errorf("обои %q: область размытия не шире той, что по умолчанию: %s", name, tag)
			}
		}
	}
	if filters < 3 {
		t.Fatalf("размытий на обоях подозрительно мало: %d", filters)
	}
}
