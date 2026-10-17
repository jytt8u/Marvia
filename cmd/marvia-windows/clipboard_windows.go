//go:build windows

package main

import (
	"errors"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Буфер обмена — через Windows, а не через страницу.
//
// navigator.clipboard у страницы WebView2 работает не везде: права на запись
// выдаёт движок, и «Write permission denied» на кнопке «Скопировать отчёт» —
// это кнопка, которая не делает обещанного. Программе Windows разрешает
// писать в буфер всегда.

var (
	openClipboard    = user32.NewProc("OpenClipboard")
	closeClipboard   = user32.NewProc("CloseClipboard")
	emptyClipboard   = user32.NewProc("EmptyClipboard")
	setClipboardData = user32.NewProc("SetClipboardData")
	globalAlloc      = kernel32.NewProc("GlobalAlloc")
	globalFree       = kernel32.NewProc("GlobalFree")
	globalLock       = kernel32.NewProc("GlobalLock")
	globalUnlock     = kernel32.NewProc("GlobalUnlock")
	moveMemory       = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// copyText кладёт текст в буфер обмена Windows.
func copyText(text string) error {
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	// Буфер бывает занят другой программой долю секунды — менеджеры буфера
	// открывают его на каждое изменение. Несколько попыток вместо отказа.
	opened := false
	for i := 0; i < 10 && !opened; i++ {
		if r, _, _ := openClipboard.Call(0); r != 0 {
			opened = true
		} else {
			time.Sleep(30 * time.Millisecond)
		}
	}
	if !opened {
		return errors.New(say("clipboardBusy"))
	}
	defer closeClipboard.Call()
	emptyClipboard.Call()

	size := uintptr(len(utf16) * 2)
	mem, _, err := globalAlloc.Call(gmemMoveable, size)
	if mem == 0 {
		return err
	}
	ptr, _, err := globalLock.Call(mem)
	if ptr == 0 {
		globalFree.Call(mem)
		return err
	}
	// Копирует система: превращать адрес из GlobalLock в указатель Go — то,
	// от чего go vet справедливо предостерегает.
	moveMemory.Call(ptr, uintptr(unsafe.Pointer(&utf16[0])), size)
	globalUnlock.Call(mem)

	// После успешного SetClipboardData память принадлежит системе.
	if r, _, err := setClipboardData.Call(cfUnicodeText, mem); r == 0 {
		globalFree.Call(mem)
		return err
	}
	return nil
}
