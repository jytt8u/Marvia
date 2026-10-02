//go:build !linux

package tunnel

import (
	"net"
	"time"
)

// kernelRTT вне Linux и Android не реализован: на Windows оценка есть
// (SIO_TCP_INFO), но там отклик пока мерится по-старому, пингом внутри
// туннеля.
func kernelRTT(*net.TCPConn) (time.Duration, bool, error) { return 0, false, nil }
