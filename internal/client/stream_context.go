package client

import (
	"context"
	"errors"
	"net"
	"time"
)

// Контекст дозвона сам по себе не прерывает чтение уже открытого потока.
// Будим только этот поток: отмена замера не должна рвать чужие загрузки.
func armStream(ctx context.Context, stream net.Conn) func() {
	if deadline, ok := ctx.Deadline(); ok {
		_ = stream.SetDeadline(deadline)
	}
	done := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		_ = stream.SetDeadline(time.Now())
		close(done)
	})
	return func() {
		if !stop() {
			<-done
		}
		_ = stream.SetDeadline(time.Time{})
	}
}

func streamError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var timeout net.Error
	if errors.As(err, &timeout) && timeout.Timeout() {
		if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
			return context.DeadlineExceeded
		}
	}
	return err
}
