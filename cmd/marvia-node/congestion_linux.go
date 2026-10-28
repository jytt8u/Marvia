package main

import (
	"context"
	"log"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// congestion — алгоритм управления перегрузкой для соединений ноды.
//
// Маршрут от ноды до покупателя почти всегда дальний и с потерями: другая
// страна, мобильная сеть, фильтры по дороге. Стандартный CUBIC на каждую
// потерю вдвое сбавляет скорость и разгоняется заново, и на таком маршруте
// скачивание упирается не в канал, а в эти провалы. BBR держит скорость по
// измеренной полосе и задержке, а случайную потерю за перегрузку не считает.
//
// Ставится на слушающий сокет, и его наследует каждое принятое соединение.
// Системный алгоритм сервера не меняется: остальные службы живут как жили.
const congestion = "bbr"

// listenTCP открывает порт ноды и просит для её соединений BBR.
func listenTCP(addr string) (net.Listener, error) {
	return listenWithCongestion(addr, congestion)
}

func listenWithCongestion(addr, algo string) (net.Listener, error) {
	var setErr error
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		if err := c.Control(func(fd uintptr) {
			setErr = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_CONGESTION, algo)
		}); err != nil {
			setErr = err
		}
		// Алгоритма нет — не повод не подниматься: нода работает и на
		// стандартном, просто медленнее на плохом маршруте.
		return nil
	}}
	ln, err := lc.Listen(context.Background(), "tcp", addr)
	if err == nil && setErr != nil {
		log.Printf("%s недоступен (%v): соединения идут на алгоритме ядра по умолчанию. "+
			"Включить: modprobe tcp_bbr и добавить bbr в net.ipv4.tcp_allowed_congestion_control — "+
			"установщик ноды делает это сам", algo, setErr)
	}
	return ln, err
}
