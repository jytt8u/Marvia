package client

import (
	"context"
	mrand "math/rand/v2"

	"github.com/jytt8u/marvia/internal/vp1"
)

// PickServerName выбирает имя прикрытия так же, как дозвон под REALITY.
// Только для тестов: сам выбор живёт внутри замыкания дозвона, и добраться до
// него иначе можно было бы лишь подняв настоящую ноду.
func PickServerName(n Node) string {
	names := n.serverNames(n.SNI)
	return names[mrand.IntN(len(names))]
}

// MeasureAround — замер по кнопке «Обновить» при живом туннеле. Наружу не
// нужен: его зовёт Supervisor.Measure, а тесту нужен без поднятой панели.
func MeasureAround(ctx context.Context, nodes []Node, current *Dialer, key vp1.KeyPair, opts Options) []Measurement {
	return measureAround(ctx, nodes, current, key, opts)
}
