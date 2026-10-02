package tunnel

import (
	"net"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// sioTCPInfo — SIO_TCP_INFO из mstcpip.h: _WSAIORW(IOC_VENDOR, 39). В
// golang.org/x/sys его нет, как и структуры ниже.
const sioTCPInfo = 0xD8000027

// tcpInfoV0 — TCP_INFO_v0 из mstcpip.h, поле в поле. Порядок и типы те же,
// что в C, поэтому и выравнивание то же: Go и C кладут их одинаково.
type tcpInfoV0 struct {
	State             int32
	Mss               uint32
	ConnectionTimeMs  uint64
	TimestampsEnabled bool
	RttUs             uint32
	MinRttUs          uint32
	BytesInFlight     uint32
	Cwnd              uint32
	SndWnd            uint32
	RcvWnd            uint32
	RcvBuf            uint32
	BytesOut          uint64
	BytesIn           uint64
	BytesReordered    uint32
	BytesRetrans      uint32
	FastRetrans       uint32
	DupAcksIn         uint32
	TimeoutEpisodes   uint32
	SynRetrans        uint8
}

// tcpStateEstablished — TcpConnectionEstablished в перечислении TCPSTATE.
const tcpStateEstablished = 4

// stallTimeouts — сколько раз подряд между двумя замерами истёк таймер
// повторной отправки, пока не пришло ни байта. Времени последнего
// подтверждения, как в Linux, Windows не отдаёт, поэтому обрыв видно так.
const stallTimeouts = 2

// kernelRTT спрашивает у Windows сглаженный отклик соединения.
func kernelRTT(tcp *net.TCPConn, st *rttState) (time.Duration, bool, error) {
	raw, err := tcp.SyscallConn()
	if err != nil {
		return 0, false, nil
	}
	info, ok := tcpInfo(raw)
	if !ok {
		return 0, false, nil
	}

	st.mu.Lock()
	stalled := st.seen && info.BytesInFlight > 0 &&
		info.TimeoutEpisodes >= st.timeouts+stallTimeouts && info.BytesIn == st.bytesIn
	st.seen, st.timeouts, st.bytesIn = true, info.TimeoutEpisodes, info.BytesIn
	st.mu.Unlock()

	if info.State != tcpStateEstablished || stalled {
		return 0, true, errStalled
	}
	if info.RttUs == 0 {
		return 0, false, nil
	}
	return time.Duration(info.RttUs) * time.Microsecond, true, nil
}

// tcpInfo — TCP_INFO_v0 сокета. false — Windows его не отдал: система раньше
// 1703 или сокет уже закрыт.
func tcpInfo(raw interface{ Control(func(uintptr)) error }) (tcpInfoV0, bool) {
	var (
		info    tcpInfoV0
		version uint32 // 0 — TCP_INFO_v0, есть с Windows 10 1703
		got     uint32
		ioErr   error
	)
	if err := raw.Control(func(fd uintptr) {
		ioErr = windows.WSAIoctl(windows.Handle(fd), sioTCPInfo,
			(*byte)(unsafe.Pointer(&version)), uint32(unsafe.Sizeof(version)),
			(*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)),
			&got, nil, 0)
	}); err != nil || ioErr != nil || got == 0 {
		return tcpInfoV0{}, false
	}
	return info, true
}
