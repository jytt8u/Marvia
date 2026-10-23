package main

import (
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// Хороший шаг кнопок не получает, плохой — только из тех, что окно умеет
// делать. «Починить» под зелёной галочкой — кнопка, которой нечего чинить.
func TestOnlyTroubledStepsOfferActions(t *testing.T) {
	steps := []DiagStep{
		judgeNet(true, 12), judgeNet(false, 0),
		judgeDNS(true, true, 5), judgeDNS(false, true, 0), judgeDNS(false, false, 0),
		judgeNode(3, 3, 40), judgeNode(2, 3, 40), judgeNode(3, 3, 450), judgeNode(0, 3, 0), judgeNode(0, 1, 0), judgeNode(0, 0, 0),
		judgeTunnel("connected", false, ""), judgeTunnel("failed", false, "нет"), judgeTunnel("failed", true, "нет"),
		judgeTunnel("stalled", false, "молчит"), judgeTunnel("idle", false, ""), judgeTunnel("connecting", false, ""),
		judgeSystem(true, true, true), judgeSystem(true, false, true), judgeSystem(true, false, false), judgeSystem(false, false, false),
		judgeWhere(true, "95.216.41.165", "fi"), judgeWhere(true, "", ""), judgeWhere(false, "", ""),
		judgeSites(true, 7, 7), judgeSites(true, 5, 7), judgeSites(true, 0, 7), judgeSites(false, 0, 0),
		judgeInterference(nil, "", false), judgeInterference([]string{"WARP"}, "", false),
		judgeInterference(nil, "прокси 127.0.0.1:10809", false), judgeInterference(nil, "прокси из окружения", true),
	}
	for _, s := range steps {
		if !slices.Contains(diagSteps, s.ID) {
			t.Errorf("шаг %q не из списка diagSteps", s.ID)
		}
		if !slices.Contains([]string{diagOK, diagWarn, diagBad, diagOff}, s.St) {
			t.Errorf("%s: незнакомое состояние %q", s.ID, s.St)
		}
		if s.St == diagOK && len(s.Act) > 0 {
			t.Errorf("%s: хороший шаг предлагает %v", s.ID, s.Act)
		}
		for _, a := range s.Act {
			if !slices.Contains(diagActs, a) {
				t.Errorf("%s: действие %q окно не умеет", s.ID, a)
			}
		}
	}
}

// Где Marvia помочь не может, кнопки нет: без интернета чинить нечего, а
// прокси из переменных окружения из реестра не снять.
func TestNoButtonWhereThereIsNothingToPress(t *testing.T) {
	if s := judgeNet(false, 0); len(s.Act) != 0 {
		t.Errorf("нет интернета, а предлагается %v", s.Act)
	}
	if s := judgeInterference(nil, "прокси", true); slices.Contains(s.Act, "proxy") {
		t.Error("прокси из окружения, а кнопка «убрать» на месте")
	}
	if s := judgeNode(0, 1, 0); slices.Contains(s.Act, "nodes") {
		t.Error("сервер один, а предлагается сменить его на другой")
	}
}

// Дробление предлагается, только пока оно выключено: включённое второй раз
// не включишь, а кнопка, которая ничего не меняет, — обман.
func TestSplittingIsOfferedOnlyWhenOff(t *testing.T) {
	if s := judgeTunnel("failed", false, ""); !slices.Contains(s.Act, "fragment") {
		t.Errorf("не подключилось без дробления, а его не предлагают: %v", s.Act)
	}
	if s := judgeTunnel("failed", true, ""); slices.Contains(s.Act, "fragment") {
		t.Errorf("дробление уже включено, а его предлагают снова: %v", s.Act)
	}
}

// Каждый шаг программы есть в окне — в том же порядке и со словами на обоих
// языках; каждое действие — с надписью на обоих языках и с делом за кнопкой.
func TestEveryDiagStepAndActionIsSpokenAndDone(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	page := string(raw)

	m := regexp.MustCompile(`const DIAG_STEPS = \[([^\]]*)\]`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("в окне нет списка шагов DIAG_STEPS")
	}
	var window []string
	for _, q := range regexp.MustCompile(`"([a-z]+)"`).FindAllStringSubmatch(m[1], -1) {
		window = append(window, q[1])
	}
	if !slices.Equal(window, diagSteps) {
		t.Errorf("шаги в окне %v, у программы %v", window, diagSteps)
	}

	for _, lang := range []string{"ru", "en"} {
		dict := dictionaryBlock(t, page, lang)
		steps := innerBlock(t, dict, "diagStep: {")
		for _, id := range diagSteps {
			if !regexp.MustCompile(`(?m)^\s*` + id + `: \{ t: "[^"]+"`).MatchString(steps) {
				t.Errorf("%s: у шага %q нет названия", lang, id)
			}
		}
		acts := innerBlock(t, dict, "diagAct: {")
		for _, a := range diagActs {
			if !regexp.MustCompile(`\b` + a + `: "[^"]+"`).MatchString(acts) {
				t.Errorf("%s: у действия %q нет надписи", lang, a)
			}
		}
	}

	do := innerBlock(t, page, "const DIAG_DO = {")
	for _, a := range diagActs {
		if !regexp.MustCompile(`(?m)^\s*` + a + `: `).MatchString(do) {
			t.Errorf("у действия %q нет дела в DIAG_DO — кнопка была бы нарисованной", a)
		}
	}
}

// dictionaryBlock — словарь одного языка целиком.
func dictionaryBlock(t *testing.T, page, lang string) string {
	t.Helper()
	head := "\n  " + lang + ": {"
	start := strings.Index(page, head)
	if start < 0 {
		t.Fatalf("в странице нет словаря %s", lang)
	}
	rest := page[start+len(head):]
	end := len(rest)
	for _, mark := range []string{"\n  en: {", "\n  ru: {", "\n};"} {
		if at := strings.Index(rest, mark); at >= 0 && at < end {
			end = at
		}
	}
	return rest[:end]
}

// innerBlock — содержимое фигурных скобок после head, с учётом вложенных.
func innerBlock(t *testing.T, text, head string) string {
	t.Helper()
	start := strings.Index(text, head)
	if start < 0 {
		t.Fatalf("нет блока %q", head)
	}
	depth := 0
	for i := start + len(head) - 1; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start+len(head) : i]
			}
		}
	}
	t.Fatalf("блок %q не закрыт", head)
	return ""
}

// Страница начинается с <!doctype html>. Строчка кода, вставленная перед
// ним, уводит WebView2 в режим совместимости и рисуется текстом над окном —
// так было в выпусках с 0.13.0-alpha.22 по alpha.24.
func TestPageStartsWithDoctype(t *testing.T) {
	raw, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(strings.ToLower(string(raw)), "<!doctype html>") {
		head := string(raw)
		if len(head) > 80 {
			head = head[:80]
		}
		t.Errorf("страница начинается не с <!doctype html>: %q", head)
	}
}
