package main

import (
	"os"
	"strings"
	"testing"
)

const (
	testKeyA = "vless://11111111-2222-3333-4444-555555555555@nl.example.com:443?security=tls&type=tcp#Нидерланды"
	testKeyB = "https://panel.other.example/sub/abcdef"
)

// Список переживает перезапуск, ключ не двоится, а рабочий со старой версии
// оказывается в списке сам.
func TestKeysSurviveRestartAndDoNotDuplicate(t *testing.T) {
	dir := t.TempDir()
	list := loadKeys(dir, testKeyA)
	if len(list) != 1 || list[0].Name != "Нидерланды" {
		t.Fatalf("после обновления со старой версии: %+v", list)
	}
	list = withKey(list, "", testKeyB)
	list = withKey(list, "", "  "+testKeyB+"  ")
	if len(list) != 2 || list[1].Name != "panel.other.example" {
		t.Fatalf("после второго ключа: %+v", list)
	}
	list = withKey(list, "Работа", testKeyB)
	if err := saveKeys(dir, list); err != nil {
		t.Fatal(err)
	}
	again := loadKeys(dir, testKeyA)
	if len(again) != 2 || again[1].Name != "Работа" {
		t.Errorf("после перезапуска: %+v", again)
	}
}

// Окно видит отпечатки, а не ссылки: в ссылке личный ключ покупателя.
func TestWindowSeesFingerprintsNotLinks(t *testing.T) {
	list := withKey(withKey(nil, "", testKeyA), "", testKeyB)
	views := keyViews(list, testKeyB)
	if len(views) != 2 || views[0].Active || !views[1].Active || !views[0].Foreign {
		t.Fatalf("виды: %+v", views)
	}
	for _, v := range views {
		if strings.Contains(v.ID, "example") || len(v.ID) != 12 {
			t.Errorf("отпечаток %q похож на ссылку", v.ID)
		}
	}
	if left := withoutKey(list, keyID(testKeyA)); len(left) != 1 || left[0].Link != testKeyB {
		t.Errorf("после удаления: %+v", left)
	}
}

// Битый файл списка не теряет рабочий ключ.
func TestBrokenKeyListKeepsTheActiveKey(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(keysPath(dir), []byte("[{сломано"), 0o600); err != nil {
		t.Fatal(err)
	}
	if list := loadKeys(dir, testKeyB); len(list) != 1 || list[0].Link != testKeyB {
		t.Errorf("прочитано %+v", list)
	}
}

// Имя ключа без подсказки человека — метка из ссылки или домен продавца, а
// не «Ключ 1»: по нему продавцов и отличают.
func TestKeyNameIsTheLabelOrTheSellerDomain(t *testing.T) {
	cases := map[string]string{
		testKeyA:                           "Нидерланды",
		testKeyB:                           "panel.other.example",
		"trojan://pass@de.example.net:443": "de.example.net",
		"мусор":                            "—",
	}
	for link, want := range cases {
		if got := keyName(link); got != want {
			t.Errorf("%q: %q, ожидалось %q", link, got, want)
		}
	}
}
