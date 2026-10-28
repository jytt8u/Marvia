//go:build windows && !(amd64 || arm64)

package killswitch

import "errors"

// Guard на 32-битной Windows не ставится: раскладка структур WFP там другая,
// а проверить её не на чем. Честный отказ лучше фильтра, который молча
// прочитал бы не те поля.
type Guard struct{}

func Start([]Rule) (*Guard, error) {
	return nil, errors.New("kill switch есть только в 64-битной сборке")
}

func (g *Guard) Close() error { return nil }
