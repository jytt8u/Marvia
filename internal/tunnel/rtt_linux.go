package tunnel

import (
	"net"
	"time"

	"golang.org/x/sys/unix"
)

// tcpEstablished — состояние TCP_ESTABLISHED в tcp_info.
const tcpEstablished = 1

// Когда соединение считается замолчавшим. Ядро продолжает отдавать прежнюю
// оценку отклика и после обрыва сети — подтверждений нет, новых образцов
// тоже, — поэтому без этой проверки вернулась бы старая беда: на экране
// бодрые 30 мс, а туннель мёртв.
const (
	// stallAckGap — данные отправлены, а подтверждений нет столько
	// миллисекунд. В мобильной сети пауза в секунду-две бывает и у живого
	// соединения; пять — уже нет.
	stallAckGap = 5000
	// stallBackoff — сколько раз подряд истёк таймер повторной отправки.
	// Один раз — потеря пакета, обычное дело; два подряд — сеть пропала.
	stallBackoff = 2
)

// kernelRTT спрашивает у ядра сглаженный отклик соединения.
//
// ok = false — ядро не ответило, и отклик надо мерить по-старому. err —
// соединение молчит, показывать прежнее число нельзя.
func kernelRTT(tcp *net.TCPConn) (rtt time.Duration, ok bool, err error) {
	raw, err := tcp.SyscallConn()
	if err != nil {
		return 0, false, nil
	}
	var (
		info    *unix.TCPInfo
		infoErr error
	)
	if err := raw.Control(func(fd uintptr) {
		info, infoErr = unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	}); err != nil || infoErr != nil || info == nil {
		return 0, false, nil
	}
	if info.State != tcpEstablished ||
		info.Backoff >= stallBackoff ||
		(info.Unacked > 0 && info.Last_ack_recv > stallAckGap) {
		return 0, true, errStalled
	}
	if info.Rtt == 0 {
		return 0, false, nil
	}
	return time.Duration(info.Rtt) * time.Microsecond, true, nil
}
