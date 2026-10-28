//go:build windows

package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestTheWindowGrowsWithTheMonitorScaleButNeverLeavesTheWorkArea(t *testing.T) {
	// 2560×1440 на 100 %: окно макета целиком и по центру.
	x, y, w, h := fullPlacement(rect{0, 0, 2560, 1392}, 96)
	if w != fullWidth || h != fullHeight || x != (2560-fullWidth)/2 || y != (1392-fullHeight)/2 {
		t.Fatalf("на 100 %% окно %d×%d в (%d,%d)", w, h, x, y)
	}
	// 2560×1440 на 150 %: те же точки, в полтора раза больше пикселей.
	if _, _, w, h = fullPlacement(rect{0, 0, 2560, 1380}, 144); w != 1770 || h != 1080 {
		t.Fatalf("на 150 %% окно %d×%d, а не 1770×1080", w, h)
	}
	// Ноутбук 1920×1080 на 150 %, панель задач снизу: 1770×1080 не
	// помещается — окно ужимается по высоте и не уходит за верх экрана.
	wa := rect{0, 0, 1920, 1032}
	x, y, w, h = fullPlacement(wa, 144)
	if y < int(wa.Top) || y+h > int(wa.Bottom) || x < int(wa.Left) || x+w > int(wa.Right) {
		t.Fatalf("окно %d×%d в (%d,%d) вышло за рабочую область %+v", w, h, x, y, wa)
	}
	if w != 1770 {
		t.Fatalf("по ширине место есть, а окно %d", w)
	}
	// Панель задач слева: рабочая область начинается не с нуля.
	wa = rect{80, 0, 1920, 1080}
	if x, _, _, _ = fullPlacement(wa, 144); x < 80 {
		t.Fatalf("окно залезло под панель задач слева: x=%d", x)
	}
}

func TestTheProgramTellsWindowsItDrawsItselfAtAnyScale(t *testing.T) {
	// Без манифеста Windows считает программу незнакомой с масштабом и на
	// 125–150 % растягивает окно картинкой: текст мутный, а размер окна —
	// в чужих пикселях. Права при этом — как у запустившего: администратора
	// программа просит сама, перезапуском.
	raw, err := os.ReadFile("winres.json")
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Manifest map[string]map[string]struct {
			DPI       string `json:"dpi-awareness"`
			Execution string `json:"execution-level"`
		} `json:"RT_MANIFEST"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	m, ok := res.Manifest["#1"]["0409"]
	if !ok {
		t.Fatal("в ресурсах нет манифеста")
	}
	if m.DPI != "per monitor v2" {
		t.Fatalf("масштаб объявлен как %q", m.DPI)
	}
	if m.Execution != "as invoker" {
		t.Fatalf("уровень прав %q: программа просит администратора сама, перезапуском", m.Execution)
	}
}
