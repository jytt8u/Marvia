//go:build windows

package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Образ «Прозрачная» показывает обои рабочего стола — настоящие, те, что
// стоят у человека, — ровно в том месте, где окно стоит на экране. Выглядит
// как прозрачное окно, но без системного Acrylic: тот размывал слишком
// сильно, работал только в новой Windows 11 и белел, стоило окну потерять
// фокус, — например, на снимке экрана.

var (
	procClientToScreen    = user32.NewProc("ClientToScreen")
	procMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW   = user32.NewProc("GetMonitorInfoW")
)

const (
	spiGetDeskWallpaper     = 0x0073
	monitorDefaultToNearest = 2
	wmMove                  = 0x0003
	// wallpaperMax — больше этого обои не отдаём: файл целиком идёт в
	// страницу, а полугигабайтная картинка подвесила бы окно.
	wallpaperMax = 64 << 20
)

// desktopWallpaper — путь к обоям рабочего стола. У слайд-шоу и «Windows:
// интересное» это перекодированная копия текущей картинки, и она тоже
// читается.
func desktopWallpaper() (string, error) {
	buf := make([]uint16, windows.MAX_LONG_PATH)
	r, _, err := procSystemParametersInfo.Call(spiGetDeskWallpaper, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])), 0)
	if r == 0 {
		return "", fmt.Errorf("обои не прочитались: %w", err)
	}
	path := windows.UTF16ToString(buf)
	if path == "" {
		return "", errors.New("у рабочего стола нет обоев — только цвет")
	}
	return path, nil
}

// wallpaper отдаёт обои рабочего стола странице. Только картинку и только
// этот файл: путь берётся у системы, не из адреса.
func (u *ui) wallpaper(w http.ResponseWriter, _ *http.Request) {
	path, err := desktopWallpaper()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > wallpaperMax {
		http.Error(w, "обои не читаются", http.StatusNotFound)
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		http.Error(w, "обои не читаются", http.StatusNotFound)
		return
	}
	kind := http.DetectContentType(raw)
	if !strings.HasPrefix(kind, "image/") {
		http.Error(w, "обои не картинка", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", kind)
	// Обои меняются, когда захочет человек, — кэш показал бы старые.
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(raw)
}

type monitorInfo struct {
	size    uint32
	monitor rect
	work    rect
	flags   uint32
}

// place сообщает странице, где окно стоит на своём мониторе: начало
// клиентской области относительно угла монитора и размер монитора, в
// физических пикселях, и масштаб. По этому страница сдвигает обои так, что
// они совпадают с рабочим столом вокруг окна.
func (s *shell) place() {
	if !s.loaded || s.widget {
		return
	}
	var pt struct{ X, Y int32 }
	_, _, _ = procClientToScreen.Call(s.hwnd, uintptr(unsafe.Pointer(&pt)))
	mon, _, _ := procMonitorFromWindow.Call(s.hwnd, monitorDefaultToNearest)
	mi := monitorInfo{size: uint32(unsafe.Sizeof(monitorInfo{}))}
	if r, _, _ := procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&mi))); r == 0 {
		return
	}
	dpi, _, _ := procGetDpiForWindow.Call(s.hwnd)
	if dpi == 0 {
		dpi = 96
	}
	s.w.Eval(fmt.Sprintf("window.marviaPlace && window.marviaPlace(%d,%d,%d,%d,%d)",
		pt.X-mi.monitor.Left, pt.Y-mi.monitor.Top, mi.monitor.Right-mi.monitor.Left, mi.monitor.Bottom-mi.monitor.Top, dpi))
}
