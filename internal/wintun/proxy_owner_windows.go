package wintun

import (
	"encoding/binary"
	"net"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Назвать виновника по имени — половина пользы предупреждения.
//
// «В системе прописан прокси 127.0.0.1:12334» человеку не говорит ничего: он
// не помнит, что это и откуда взялось. «Прокси 127.0.0.1:12334 (Hiddify)» —
// говорит всё сразу, и дальше он знает, где выключать.

var (
	iphlpapi            = windows.NewLazySystemDLL("iphlpapi.dll")
	getExtendedTCPTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

// tcpTableOwnerPIDListener — вид таблицы: только слушающие сокеты с их
// процессами.
const tcpTableOwnerPIDListener = 3

// tcpRow — строка таблицы соединений. Порядок полей задан системой.
type tcpRow struct {
	state      uint32
	localAddr  uint32
	localPort  uint32
	remoteAddr uint32
	remotePort uint32
	owningPID  uint32
}

// ownerOf выясняет, какая программа слушает адрес прокси.
//
// Пустая строка — обычный ответ, а не ошибка: прокси может стоять на другой
// машине, а процесс — принадлежать другому пользователю. Предупреждение и без
// имени остаётся верным, поэтому здесь мы ничего не ломаем молчанием.
// Второе значение — слушает ли порт хоть кто-то, пусть и безымянный.
func ownerOf(server string) (string, bool) {
	port, local := parseProxyPort(server)
	if !local || port == 0 {
		return "", false
	}

	pid, ok := listenerPID(port)
	if !ok {
		return "", false
	}
	return processName(pid), true
}

// parseProxyPort достаёт порт и отвечает, на этой ли машине прокси.
//
// Строка бывает разной: "127.0.0.1:12334", "http://127.0.0.1:12334" и даже
// перечислением через точку с запятой — "http=…;https=…". Разбираем терпимо:
// цель не проверить синтаксис, а найти порт.
func parseProxyPort(server string) (uint16, bool) {
	value := server
	if i := strings.IndexByte(value, ';'); i >= 0 {
		value = value[:i]
	}
	if i := strings.Index(value, "="); i >= 0 {
		value = value[i+1:]
	}
	value = strings.TrimPrefix(strings.TrimPrefix(value, "http://"), "https://")
	value = strings.TrimSuffix(value, "/")

	host, portText, err := net.SplitHostPort(value)
	if err != nil {
		return 0, false
	}
	port, err := strconv.ParseUint(portText, 10, 16)
	if err != nil {
		return 0, false
	}

	ip := net.ParseIP(host)
	return uint16(port), host == "localhost" || (ip != nil && ip.IsLoopback())
}

// tcp6Row — строка таблицы IPv6: адрес, область, порт, удалённая сторона,
// состояние и процесс. Порядок полей задан системой.
type tcp6Row struct {
	localAddr   [16]byte
	localScope  uint32
	localPort   uint32
	remoteAddr  [16]byte
	remoteScope uint32
	remotePort  uint32
	state       uint32
	owningPID   uint32
}

// listenerPID ищет процесс, слушающий этот порт — в таблице IPv4, а затем
// IPv6: прокси, слушающий только [::1], иначе выглядел бы мёртвым, и окно
// назвало бы его причиной, хотя он работает.
func listenerPID(port uint16) (uint32, bool) {
	if pid, ok := listenerPID4(port); ok {
		return pid, true
	}
	return listenerPID6(port)
}

// portOf достаёт порт из поля строки: младшие два байта в сетевом порядке.
func portOf(field uint32) uint16 {
	var raw [4]byte
	binary.LittleEndian.PutUint32(raw[:], field)
	return binary.BigEndian.Uint16(raw[:2])
}

// tcpTable читает таблицу слушающих сокетов одного семейства адресов.
func tcpTable(family uintptr) []byte {
	var size uint32
	_, _, _ = getExtendedTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0, family, tcpTableOwnerPIDListener, 0)
	if size == 0 {
		return nil
	}
	buf := make([]byte, size)
	ret, _, _ := getExtendedTCPTable.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0, family, tcpTableOwnerPIDListener, 0)
	if ret != 0 || len(buf) < 4 {
		return nil
	}
	return buf
}

func listenerPID6(port uint16) (uint32, bool) {
	buf := tcpTable(windows.AF_INET6)
	if buf == nil {
		return 0, false
	}
	count := binary.LittleEndian.Uint32(buf)
	if uintptr(len(buf)-4) < uintptr(count)*unsafe.Sizeof(tcp6Row{}) {
		return 0, false
	}
	for _, row := range unsafe.Slice((*tcp6Row)(unsafe.Pointer(&buf[4])), count) {
		if portOf(row.localPort) == port {
			return row.owningPID, true
		}
	}
	return 0, false
}

func listenerPID4(port uint16) (uint32, bool) {
	var size uint32

	// Первый вызов только узнаёт размер: таблица меняется между вызовами, и
	// угадывать её длину заранее нельзя.
	_, _, _ = getExtendedTCPTable.Call(0, uintptr(unsafe.Pointer(&size)), 0,
		windows.AF_INET, tcpTableOwnerPIDListener, 0)
	if size == 0 {
		return 0, false
	}

	buf := make([]byte, size)
	ret, _, _ := getExtendedTCPTable.Call(uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)), 0, windows.AF_INET, tcpTableOwnerPIDListener, 0)
	if ret != 0 {
		return 0, false
	}

	count := binary.LittleEndian.Uint32(buf)
	rows := unsafe.Slice((*tcpRow)(unsafe.Pointer(&buf[4])), count)

	for _, row := range rows {
		// Порт лежит в младших двух байтах в сетевом порядке.
		var raw [4]byte
		binary.LittleEndian.PutUint32(raw[:], row.localPort)
		if binary.BigEndian.Uint16(raw[:2]) == port {
			return row.owningPID, true
		}
	}
	return 0, false
}

// processName возвращает имя программы без расширения и пути.
func processName(pid uint32) string {
	snapshot, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(snapshot)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))

	if err := windows.Process32First(snapshot, &entry); err != nil {
		return ""
	}
	for {
		if entry.ProcessID == pid {
			name := windows.UTF16ToString(entry.ExeFile[:])
			return strings.TrimSuffix(name, ".exe")
		}
		if err := windows.Process32Next(snapshot, &entry); err != nil {
			return ""
		}
	}
}
