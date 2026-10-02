//go:build !linux

package main

import "net"

// listenTCP вне Linux — обычный порт: выбор алгоритма перегрузки на сокет
// есть только у Linux, а нода в бою стоит только там.
func listenTCP(addr string) (net.Listener, error) { return net.Listen("tcp", addr) }
