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

var (
	getClipboardData = user32.NewProc("GetClipboardData")
	clipboardHasFmt  = user32.NewProc("IsClipboardFormatAvailable")
	globalSize       = kernel32.NewProc("GlobalSize")
)

// openClip открывает буфер. Он бывает занят другой программой долю секунды
// — менеджеры буфера открывают его на каждое изменение. Несколько попыток
// вместо отказа.
func openClip() bool {
	for i := 0; i < 10; i++ {
		if r, _, _ := openClipboard.Call(0); r != 0 {
			return true
		}
		time.Sleep(30 * time.Millisecond)
	}
	return false
}

// pasteText — текст из буфера обмена Windows; пусто, если там не текст или
// текст больше любого ключа. Читает программа, а не страница, по той же
// причине, что и пишет: у WebView2 права на буфер бывают отняты.
func pasteText() (string, error) {
	if r, _, _ := clipboardHasFmt.Call(cfUnicodeText); r == 0 {
		return "", nil
	}
	if !openClip() {
		return "", errors.New(say("clipboardBusy"))
	}
	defer closeClipboard.Call()
	h, _, _ := getClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return "", nil
	}
	size, _, _ := globalSize.Call(h)
	if size < 2 || size > 2*clipOfferMax+2 {
		return "", nil
	}
	ptr, _, _ := globalLock.Call(h)
	if ptr == 0 {
		return "", nil
	}
	defer globalUnlock.Call(h)
	buf := make([]uint16, size/2)
	// Копирует система — как и при записи: адрес из GlobalLock в указатель Go
	// не превращаем.
	moveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), ptr, uintptr(len(buf)*2))
	return windows.UTF16ToString(buf), nil
}

// copyText кладёт текст в буфер обмена Windows.
func copyText(text string) error {
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return err
	}
	if !openClip() {
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
