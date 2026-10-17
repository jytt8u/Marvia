//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// serviceWait — сколько ждать, пока служба остановится или запустится.
// winws выходит за доли секунды; дольше ждать — держать подключение ради
// службы, которая не отвечает.
const serviceWait = 10 * time.Second

func windowsServiceRunning(name string) bool {
	m, err := mgr.Connect()
	if err != nil {
		return false
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return false
	}
	defer s.Close()
	st, err := s.Query()
	return err == nil && st.State == svc.Running
}

func windowsStopService(name string) error {
	return controlService(name, func(s *mgr.Service) error {
		_, err := s.Control(svc.Stop)
		return err
	}, svc.Stopped)
}

func windowsStartService(name string) error {
	return controlService(name, func(s *mgr.Service) error { return s.Start() }, svc.Running)
}

func controlService(name string, act func(*mgr.Service) error, want svc.State) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(name)
	if err != nil {
		return err
	}
	defer s.Close()
	if err := act(s); err != nil {
		return err
	}
	for deadline := time.Now().Add(serviceWait); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if st, err := s.Query(); err == nil && st.State == want {
			return nil
		}
	}
	return errors.New("служба не ответила вовремя")
}

// warpCLI — консольная утилита WARP. Своего API у WARP нет: всё, что умеет
// его окно, делается через неё же.
func warpCLI() string {
	return filepath.Join(os.Getenv("ProgramFiles"), "Cloudflare", "Cloudflare WARP", "warp-cli.exe")
}

func runWarp(args ...string) (string, error) {
	path := warpCLI()
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	cmd := exec.Command(path, args...)
	// Без окна консоли: иначе при каждом подключении мигало бы чёрное окно.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NO_WINDOW}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func cliWarpConnected() bool {
	out, err := runWarp("status")
	return err == nil && warpSaysConnected(out)
}

// warpSaysConnected разбирает «Status update: Connected». «Disconnected»
// тоже содержит «Connected», поэтому сравниваем значение целиком.
func warpSaysConnected(out string) bool {
	for _, line := range strings.Split(out, "\n") {
		if _, value, ok := strings.Cut(line, "Status update:"); ok {
			return strings.TrimSpace(value) == "Connected"
		}
	}
	return false
}

func cliSetWarp(on bool) error {
	verb := "disconnect"
	if on {
		verb = "connect"
	}
	_, err := runWarp(verb)
	return err
}

// terminateByName закрывает процессы с этими именами (без .exe, в нижнем
// регистре) и отвечает, сколько закрыл.
func terminateByName(names []string) int {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snapshot)
	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	closed := 0
	for err := windows.Process32First(snapshot, &entry); err == nil; err = windows.Process32Next(snapshot, &entry) {
		name := strings.ToLower(strings.TrimSuffix(windows.UTF16ToString(entry.ExeFile[:]), ".exe"))
		if !want[name] {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, entry.ProcessID)
		if err != nil {
			continue
		}
		if windows.TerminateProcess(h, 0) == nil {
			closed++
		}
		windows.CloseHandle(h)
	}
	return closed
}
