//go:build windows

package main

import (
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
)

func TestZapretWarpAndASecondVPNAreNamedInTheWindow(t *testing.T) {
	// Сегодняшний случай владельца: YouTube не открывался внутри туннеля, а
	// причиной был работающий рядом zapret и ещё два VPN-клиента.
	got := conflictsAmong([]string{"explorer.exe", "winws.exe", "Cloudflare WARP.exe", "warp-svc.exe", "Hiddify.exe"})
	want := []string{"conflictWarp", "conflictHiddify"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("найдено %v, ожидалось %v", got, want)
	}
	if len(conflictsAmong([]string{"explorer.exe", "chrome.exe", "marvia-windows.exe"})) != 0 {
		t.Fatal("обычные программы названы мешающими")
	}

	old := processList
	t.Cleanup(func() { processList = old })
	processList = func() []string { return []string{"winws.exe", "Hiddify.exe"} }
	c := &Controller{state: StateIdle, node: client.Node{}, log: newJournal(), proxyAt: time.Now()}
	c.refreshConflicts()
	s := c.Status()
	if len(s.Conflicts) != 1 || !strings.Contains(s.Conflicts[0], "Hiddify") || !strings.Contains(s.Conflicts[0], "«Починить»") {
		t.Fatalf("окно не получило понятного сообщения про zapret: %v", s.Conflicts)
	}
}
