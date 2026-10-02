//go:build unix

package main

import (
	"os"
	"syscall"
)

// keepOwner возвращает файлам базы хозяина самой базы.
//
// Перенос запускают от root: копия прежней базы лежит в /root, и читать её
// службе незачем. Но SQLite при записи заводит рядом -wal и -shm от имени
// того, кто пишет, — и служба панели, работающая от marvia, после этого не
// поднимется, а увидит продавец это утром, по молчащей панели.
func keepOwner(db string) {
	if os.Geteuid() != 0 {
		return
	}
	info, err := os.Stat(db)
	if err != nil {
		return
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return
	}
	for _, path := range []string{db, db + "-wal", db + "-shm"} {
		_ = os.Chown(path, int(st.Uid), int(st.Gid))
	}
}
