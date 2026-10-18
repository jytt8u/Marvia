package tunbridge

import (
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// Приём TCP — свой, а не tun2socks, ради одного: IPv6-соединение, которое
// нода не повезёт, должно получить отказ ещё на SYN.
//
// tun2socks сначала завершает рукопожатие, а уже потом отдаёт соединение
// мосту. Мост закрывал его сразу, но приложение к тому времени считало его
// установленным: браузер получал ERR_CONNECTION_CLOSED и на IPv4 не
// переходил — для него сайт ответил и оборвал. Так не грузились картинки
// GitHub, Fastly и всё, у чего есть IPv6-адрес, если браузер узнавал его
// сам, по зашифрованному DNS, мимо нашего пустого ответа на AAAA. Отказ на
// SYN — это RST, ровно то, что приложение видит от выключенного порта, и
// Happy Eyeballs тут же берёт IPv4.

// Те же значения, что у tun2socks: окно по умолчанию, предел одновременных
// рукопожатий и keepalive.
const (
	tcpMaxConnAttempts   = 2 << 10
	tcpKeepaliveCount    = 9
	tcpKeepaliveIdle     = 60 * time.Second
	tcpKeepaliveInterval = 30 * time.Second
)

// acceptTCP ставит свой приём поверх того, что поставил tun2socks.
func (h *handler) acceptTCP(s *stack.Stack) {
	f := tcp.NewForwarder(s, 0, tcpMaxConnAttempts, func(r *tcp.ForwarderRequest) {
		var wq waiter.Queue
		id := r.ID()
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			r.Complete(true)
			return
		}
		defer r.Complete(false)
		keepalive(s, ep)
		h.HandleTCP(&tcpConn{TCPConn: gonet.NewTCPConn(&wq, ep), id: id})
	})
	s.SetTransportProtocolHandler(tcp.ProtocolNumber, func(id stack.TransportEndpointID, pkt *stack.PacketBuffer) bool {
		// false — «никого нет»: стек сам отвечает RST.
		if id.LocalAddress.Len() == header.IPv6AddressSize && h.v6.off.Load() {
			return false
		}
		return f.HandlePacket(id, pkt)
	})
}

func keepalive(s *stack.Stack, ep tcpip.Endpoint) {
	ep.SocketOptions().SetKeepAlive(true)
	idle := tcpip.KeepaliveIdleOption(tcpKeepaliveIdle)
	_ = ep.SetSockOpt(&idle)
	interval := tcpip.KeepaliveIntervalOption(tcpKeepaliveInterval)
	_ = ep.SetSockOpt(&interval)
	_ = ep.SetSockOptInt(tcpip.KeepaliveCountOption, tcpKeepaliveCount)

	var ss tcpip.TCPSendBufferSizeRangeOption
	if err := s.TransportProtocolOption(header.TCPProtocolNumber, &ss); err == nil {
		ep.SocketOptions().SetSendBufferSize(int64(ss.Default), false)
	}
	var rs tcpip.TCPReceiveBufferSizeRangeOption
	if err := s.TransportProtocolOption(header.TCPProtocolNumber, &rs); err == nil {
		ep.SocketOptions().SetReceiveBufferSize(int64(rs.Default), false)
	}
}

// tcpConn — соединение для моста: поток и адреса, с которых он пришёл.
type tcpConn struct {
	*gonet.TCPConn
	id stack.TransportEndpointID
}

func (c *tcpConn) ID() stack.TransportEndpointID { return c.id }
