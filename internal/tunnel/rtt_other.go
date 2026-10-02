//go:build !linux && !windows

package tunnel

import (
	"net"
	"time"
)

// kernelRTT на прочих системах не реализован: отклик мерится по-старому,
// пингом внутри туннеля. Клиенты Marvia работают на Android и Windows, а
// нода — на Linux, так что сюда попадает разве что сборка для отладки.
func kernelRTT(*net.TCPConn, *rttState) (time.Duration, bool, error) { return 0, false, nil }
