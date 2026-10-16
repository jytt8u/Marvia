//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// runningProcesses — имена запущенных программ. Снимок системы, без прав
// администратора и без чтения чужой памяти: только имена.
func runningProcesses() []string {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	if err := windows.Process32First(snapshot, &entry); err != nil {
		return nil
	}
	var names []string
	for {
		names = append(names, windows.UTF16ToString(entry.ExeFile[:]))
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			return names
		}
	}
}
