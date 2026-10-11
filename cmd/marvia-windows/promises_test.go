package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// «Чего пока нет» в окне и в README — один и тот же список. Обзор в
// настройках говорит человеку то же, что README говорит до установки:
// честность «чего нет» — защита от продажи несуществующего, и она не
// должна расходиться между двумя местами.
func TestWindowAndReadmeAgreeOnWhatWindowsLacks(t *testing.T) {
	page, err := os.ReadFile("ui/app.html")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	readme := strings.ReplaceAll(string(raw), "\r\n", "\n")
	start := strings.Index(readme, "- На Windows —")
	if start < 0 {
		t.Fatal("в README нет строки о том, чего нет на Windows")
	}
	end := strings.Index(readme[start+1:], "\n- ")
	gap := strings.ToLower(readme[start : start+1+end])
	// Обещание в окне и то, как его называет README.
	names := map[string]string{
		"killswitch": "kill switch",
		"apps":       "мимо туннеля",
		"qr":         "qr",
		"stats":      "за месяц",
	}
	for id, phrase := range names {
		lacks := regexp.MustCompile(`\["` + id + `", \(\) => "no"\]`).Match(page)
		listed := strings.Contains(gap, phrase)
		if lacks != listed {
			t.Errorf("%s: в окне «пока нет» = %v, а в README среди недостающего = %v", id, lacks, listed)
		}
		if !strings.Contains(string(page), `["`+id+`",`) {
			t.Errorf("обещания %q нет в обзоре окна", id)
		}
	}
}
