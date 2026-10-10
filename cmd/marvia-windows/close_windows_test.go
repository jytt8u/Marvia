//go:build windows

package main

import "testing"

// Крестик без выбора прячет окно в трей: туннель продолжает работать, и
// закрыть защиту случайным кликом нельзя. Закрывает программу — только по
// явному выбору в настройках.
func TestCloseHidesToTrayUnlessAskedToQuit(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	if closeQuits() {
		t.Fatal("без выбора крестик закрывает программу")
	}
	if err := writeUISetting(closeSetting, "quit"); err != nil {
		t.Fatal(err)
	}
	if !closeQuits() {
		t.Fatal("выбор «закрывать программу» не сработал")
	}
	if err := writeUISetting(closeSetting, "tray"); err != nil {
		t.Fatal(err)
	}
	if closeQuits() {
		t.Fatal("возврат к трею не сработал")
	}
}
