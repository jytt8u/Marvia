//go:build windows

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/wintun"
)

// Тесты не трогают настоящую Windows: ни прокси, ни службы, ни WARP, ни
// чужие процессы. Без этого тест, нечаянно дошедший до починки, снял бы
// владельцу прокси или остановил бы его zapret.
func init() {
	disableProxy = func() error { return errors.New("тесты не трогают прокси Windows") }
	serviceRunning = func(string) bool { return false }
	stopService = func(string) error { return errors.New("тесты не трогают службы") }
	startService = stopService
	warpConnected = func() bool { return false }
	setWarp = func(bool) error { return errors.New("тесты не трогают WARP") }
	closePrograms = func([]string) int { return 0 }
	probeSystem = func(context.Context) error { return nil }
}

// fakeSystem — службы и WARP, как их видит pauser, без настоящей Windows.
type fakeSystem struct {
	running map[string]bool
	warp    bool
}

func fakeWindows(t *testing.T, sys *fakeSystem) {
	t.Helper()
	oldRunning, oldStop, oldStart, oldWarp, oldSet := serviceRunning, stopService, startService, warpConnected, setWarp
	t.Cleanup(func() {
		serviceRunning, stopService, startService, warpConnected, setWarp = oldRunning, oldStop, oldStart, oldWarp, oldSet
	})
	serviceRunning = func(name string) bool { return sys.running[name] }
	stopService = func(name string) error { sys.running[name] = false; return nil }
	startService = func(name string) error { sys.running[name] = true; return nil }
	warpConnected = func() bool { return sys.warp }
	setWarp = func(on bool) error { sys.warp = on; return nil }
}

func TestZapretKeepsRunningAndWarpComesBackAfterTheConnection(t *testing.T) {
	// zapret человеку нужен и без VPN, а его обманки мост выбрасывает сам:
	// останавливать его ради туннеля больше незачем.
	sys := &fakeSystem{running: map[string]bool{"zapret": true, "GoodbyeDPI": true}, warp: true}
	fakeWindows(t, sys)
	p := newPauser(filepath.Join(t.TempDir(), "paused.json"), func(string) {})

	p.pause()
	if !sys.running["zapret"] || !sys.running["GoodbyeDPI"] {
		t.Fatal("Marvia остановила zapret, который туннелю больше не мешает")
	}
	if sys.warp {
		t.Fatal("второй VPN остался подключённым на время подключения")
	}
	// Переподключение снова зовёт pause: отключённое нами не должно стать
	// «отключённым до нас» и потеряться.
	p.pause()
	p.restore()
	if !sys.warp {
		t.Fatal("после отключения WARP не вернулся")
	}
}

func TestWarpDisconnectedBeforeMarviaIsNotConnectedByIt(t *testing.T) {
	sys := &fakeSystem{running: map[string]bool{}}
	fakeWindows(t, sys)
	p := newPauser(filepath.Join(t.TempDir(), "paused.json"), func(string) {})

	p.pause()
	p.restore()
	if sys.warp {
		t.Fatal("Marvia подключила WARP, который человек выключил сам")
	}
}

func TestAfterACrashStoppedProgramsComeBackOnTheNextStart(t *testing.T) {
	// Так оставляет файл alpha.15, упавшая посреди подключения: она ещё
	// останавливала zapret. Новая версия обязана его вернуть.
	sys := &fakeSystem{running: map[string]bool{"zapret": false}, warp: false}
	fakeWindows(t, sys)
	path := filepath.Join(t.TempDir(), "paused.json")
	if err := os.WriteFile(path, []byte(`{"services":["zapret"],"warp":true}`), 0o600); err != nil {
		t.Fatal(err)
	}

	next := newPauser(path, func(string) {})
	if !next.pending() {
		t.Fatal("новый запуск не знает, что zapret и WARP остановлены")
	}
	next.restore()
	if !sys.running["zapret"] || !sys.warp || next.pending() {
		t.Fatalf("после падения не вернулось: zapret=%v warp=%v", sys.running["zapret"], sys.warp)
	}
}

func TestAbandonedProxyIsRemovedButALiveOneIsNot(t *testing.T) {
	oldRead, oldDrop := systemProxy, disableProxy
	t.Cleanup(func() { systemProxy, disableProxy = oldRead, oldDrop })
	current := wintun.Proxy{Server: "127.0.0.1:12334", Owner: "Hiddify.exe", Listening: true}
	systemProxy = func() wintun.Proxy { return current }
	dropped := 0
	disableProxy = func() error { dropped++; current = wintun.Proxy{}; return nil }
	c := &Controller{log: newJournal()}

	if c.dropDeadProxy() || dropped != 0 {
		t.Fatal("снят прокси, через который работает запущенная Hiddify")
	}
	current = wintun.Proxy{Server: "127.0.0.1:12334"}
	if !c.dropDeadProxy() || dropped != 1 {
		t.Fatal("брошенный прокси, на котором никто не отвечает, остался")
	}
}

// fakeBackend — ядро туннеля для проверки: только имя ноды.
type fakeBackend struct{ client.Backend }

func (fakeBackend) Node() client.Node { return client.Node{Name: "fi-1"} }

func TestCheckTellsAProblemOnThePCFromAProblemWithTheNode(t *testing.T) {
	oldSys, oldTun := probeSystem, probeTunnel
	t.Cleanup(func() { probeSystem, probeTunnel = oldSys, oldTun })
	broken := errors.New("не открылось")
	var sysErr, tunErr error
	probeSystem = func(context.Context) error { return sysErr }
	probeTunnel = func(context.Context, client.Backend) error { return tunErr }
	ctx := context.Background()

	if v, _ := diagnose(ctx, fakeBackend{}); v != "" {
		t.Fatalf("всё открывается, а вывод %q", v)
	}
	sysErr = broken
	if v, _ := diagnose(ctx, fakeBackend{}); v != "checkPC" {
		t.Fatalf("через ядро открывается, через Windows нет — вывод %q, а не «мешает компьютер»", v)
	}
	tunErr = broken
	if v, _ := diagnose(ctx, fakeBackend{}); v != "checkNode" {
		t.Fatalf("не открывается и через ядро — вывод %q, а не «нода»", v)
	}

	c := &Controller{state: StateConnected, connectCtx: ctx, node: client.Node{Name: "fi-1"}, log: newJournal()}
	c.keepVerdict(ctx, "checkPC")
	if s := c.Status(); !strings.Contains(s.Check, "Починить") {
		t.Fatalf("окно не предлагает починить: %q", s.Check)
	}
}

func TestFixClosesOtherVPNClientsButLeavesWarpAndZapretRunning(t *testing.T) {
	names := closable()
	for _, want := range []string{"hiddify", "v2rayn"} {
		if !slices.Contains(names, want) {
			t.Errorf("«Починить» не закрывает %s", want)
		}
	}
	for _, keep := range []string{"warp-svc", "cloudflare warp", "marvia-windows", "winws", "goodbyedpi"} {
		if slices.Contains(names, keep) {
			t.Errorf("«Починить» закрывает %s, хотя он туннелю не мешает или его достаточно отключить", keep)
		}
	}
}

func TestDisconnectedWarpIsNotCalledInterference(t *testing.T) {
	oldList, oldWarp := processList, warpConnected
	t.Cleanup(func() { processList, warpConnected = oldList, oldWarp })
	processList = func() []string { return []string{"warp-svc.exe", "Cloudflare WARP.exe"} }
	warpConnected = func() bool { return false }
	c := &Controller{log: newJournal()}

	c.refreshConflicts()
	if len(c.conflicts) != 0 {
		t.Fatalf("выключенный WARP назван помехой: %v", c.conflicts)
	}
	warpConnected = func() bool { return true }
	c.refreshConflicts()
	if !slices.Equal(c.conflicts, []string{"conflictWarp"}) {
		t.Fatalf("подключённый WARP не назван: %v", c.conflicts)
	}
}

func TestVanishedAdapterIsRaisedAgainOnceThenMarviaGivesUp(t *testing.T) {
	c := &Controller{state: StateConnected, log: newJournal()}
	c.loseAdapter()
	if c.recoveredAt.IsZero() || !strings.Contains(strings.Join(c.log.snapshot(), "\n"), say("logAdapterLost")) {
		t.Fatal("первую пропажу адаптера Marvia не попробовала исправить сама")
	}

	c.mu.Lock()
	c.state = StateConnected
	c.recoveredAt = time.Now()
	c.mu.Unlock()
	c.loseAdapter()
	if c.state != StateFailed || c.reason != say("adapterGone") {
		t.Fatalf("адаптер пропал снова за минуту, а Marvia не сдалась: %s %q", c.state, c.reason)
	}
}

func TestWarpStatusIsReadExactly(t *testing.T) {
	if !warpSaysConnected("Status update: Connected\r\nNetwork: healthy\r\n") {
		t.Fatal("подключённый WARP не узнан")
	}
	if warpSaysConnected("Status update: Disconnected\nReason: Manual Disconnection\n") {
		t.Fatal("«Disconnected» принято за подключённый WARP")
	}
}
