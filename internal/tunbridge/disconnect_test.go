package tunbridge

import (
	"sync"
	"sync/atomic"
	"testing"

	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

type countedEndpoint struct {
	stack.LinkEndpoint
	closes atomic.Int32
}

func (e *countedEndpoint) Close() { e.closes.Add(1) }

func TestDisconnectReleasesInterfaceExactlyOnce(t *testing.T) {
	device := &countedEndpoint{}
	bridge := &Bridge{device: device}
	bridge.Disconnect()
	if device.closes.Load() != 1 {
		t.Fatal("отключение не освободило интерфейс")
	}
	var workers sync.WaitGroup
	for range 20 {
		workers.Add(1)
		go func() { defer workers.Done(); bridge.Disconnect(); _ = bridge.Close() }()
	}
	workers.Wait()
	if device.closes.Load() != 1 {
		t.Fatal("повторное закрытие может задеть новый дескриптор")
	}
}
