//go:build windows

package main

import (
	"strings"
	"testing"
)

// Проводник делит аргумент по запятым на свои ключи, и ссылка продавца не
// должна превращаться в «/select» или «/root».
func TestSellerLinkCannotSmuggleExplorerSwitches(t *testing.T) {
	got := explorerArg(`https://pay.example/,/select,C:\Windows\System32`)
	if strings.Contains(got, ",") {
		t.Fatalf("в аргументе проводника осталась запятая: %s", got)
	}
	if got != `https://pay.example/%2C/select%2CC:\Windows\System32` {
		t.Fatalf("адрес изменился сверх запятых: %s", got)
	}
}
