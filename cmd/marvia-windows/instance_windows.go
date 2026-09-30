//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const wmCopyData = 0x004a

var procRtlMoveMemory = kernel32.NewProc("RtlMoveMemory")

const maxInstanceLink = 32 << 10

func instanceName() (string, error) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return "", err
	}
	return "Local\\Marvia.Client." + user.User.Sid.String(), nil
}

// Дескриптор держим до выхода: второй процесс не создаёт ещё один адаптер,
// значок и WebView. Local ограничивает блокировку текущим сеансом Windows.
func acquireInstance(name string) (windows.Handle, bool, error) {
	ptr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return 0, false, err
	}
	handle, err := windows.CreateMutex(nil, false, ptr)
	if errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return handle, false, nil
	}
	return handle, err == nil, err
}

func instanceMessage() uintptr {
	name, _ := syscall.UTF16PtrFromString("Marvia.ShowExisting")
	msg, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	return msg
}

type copyData struct {
	kind uintptr
	size uint32
	data uintptr
}

// Проверка до UAC избавляет повторный запуск от лишнего запроса прав.
// Ссылка передаётся существующему окну, которое по-прежнему спрашивает
// согласие перед заменой ключа. Никаких команд или имён файлов через IPC нет.
func forwardToRunning() (bool, error) {
	name, err := instanceName()
	if err != nil {
		return false, err
	}
	ptr, _ := windows.UTF16PtrFromString(name)
	handle, err := windows.OpenMutex(windows.SYNCHRONIZE, false, ptr)
	if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
		// Старые сборки не создавали блокировку. Не добавляем к ним ещё одну
		// копию: сначала человек завершает старую через её меню трея.
		class, _ := syscall.UTF16PtrFromString("MarviaTray")
		old, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(class)), 0)
		if old != 0 {
			return true, errors.New("уже работает старая версия Marvia; выберите «Выйти» в меню каждого её значка в трее, затем запустите новый файл")
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_ = windows.CloseHandle(handle)
	class, _ := syscall.UTF16PtrFromString("MarviaTray")
	hwnd, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(ptr)))
	if hwnd == 0 {
		return true, errors.New("Marvia уже запускается; дождитесь появления значка в трее")
	}
	var pid uint32
	_, _, _ = user32.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid != 0 {
		_, _, _ = user32.NewProc("AllowSetForegroundWindow").Call(uintptr(pid))
	}
	_, _, _ = procPostMessageW.Call(hwnd, instanceMessage(), 0, 0)
	if link := accountFromArgs(os.Args[1:]); link != "" {
		data, err := windows.UTF16FromString(link)
		if err != nil || len(data)*2 > maxInstanceLink {
			return true, errors.New("слишком длинная ссылка доступа")
		}
		payload := copyData{kind: 1, size: uint32(len(data) * 2), data: uintptr(unsafe.Pointer(&data[0]))}
		var result uintptr
		sent, _, _ := user32.NewProc("SendMessageTimeoutW").Call(hwnd, wmCopyData, 0,
			uintptr(unsafe.Pointer(&payload)), 3, 2000, uintptr(unsafe.Pointer(&result)))
		runtime.KeepAlive(data)
		runtime.KeepAlive(payload)
		if sent == 0 || result != 1 {
			return true, errors.New("окно Marvia не приняло ссылку; вставьте её в настройках")
		}
	}
	return true, nil
}

func instanceLink(payload *copyData) string {
	if payload == nil || payload.kind != 1 || payload.size < 2 || payload.size > maxInstanceLink || payload.size%2 != 0 || payload.data == 0 {
		return ""
	}
	// Данные WM_COPYDATA принадлежат Windows, а не Go: копируем их до выхода
	// из обработчика, чтобы отложенное открытие окна не держало чужую память.
	data := make([]uint16, int(payload.size/2))
	_, _, _ = procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&data[0])), payload.data, uintptr(payload.size))
	if data[len(data)-1] != 0 {
		return ""
	}
	for _, ch := range data[:len(data)-1] {
		if ch == 0 {
			return ""
		}
	}
	link := string(utf16.Decode(data[:len(data)-1]))
	if accountFromArgs([]string{link}) != link {
		return ""
	}
	return link
}

func instancePayload(address uintptr) copyData {
	var payload copyData
	_, _, _ = procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&payload)), address, unsafe.Sizeof(payload))
	return payload
}

func elevatedArguments(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = syscall.EscapeArg(arg)
	}
	return strings.Join(quoted, " ")
}

func instanceError(err error) {
	alert("Marvia", fmt.Sprintf("Не получилось открыть Marvia:\n%v", err))
}
