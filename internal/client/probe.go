package client

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Замер доступности ноды.
//
// Панель стоит за границей и видит ноду живой ровно тогда, когда для телефона
// в Иркутске она уже мертва. Единственный, кто знает правду, — само
// устройство, поэтому мерить приходится с него.
//
// Меряем не время установки TCP, а время до работающего туннеля. Разница
// принципиальная: заблокированная нода обычно принимает TCP-соединение и
// молча роняет пакеты дальше, уже во время TLS. Проверка «порт открыт»
// показала бы такую ноду живой.

const (
	// ProbeTimeout — сколько ждём одну ноду. Больше нет смысла: человек не
	// станет ждать соединения дольше нескольких секунд.
	ProbeTimeout = 8 * time.Second

	// maxParallelProbes — сколько нод щупаем одновременно.
	//
	// Ограничение не ради экономии: пачка одновременных хендшейков — сама по
	// себе примета, по которой поведенческий анализ узнаёт туннель.
	maxParallelProbes = 4

	// SpeedTimeout — сколько ждём замера скорости одной ноды.
	//
	// Короче, чем ожидание хендшейка: замер необязателен. Не успели — нода
	// просто сравнится по задержке, и это лучше, чем задержать человека на
	// экране подключения ради точности, которой он не заметит.
	SpeedTimeout = 6 * time.Second
)

// Measurement — результат замера одной ноды.
type Measurement struct {
	Node    Node
	Latency time.Duration
	// RTT — последний запрос-ответ внутри готового туннеля; ноль — нет замера.
	RTT time.Duration
	Err error

	// Fetch — за сколько нода отдала пробную порцию: круг до неё, разгон и
	// сама передача вместе. Ноль, если не мерили: живая нода одна, или она
	// старой версии и такого не умеет.
	Fetch time.Duration

	// Connect — время установки TCP для диагностики транспорта. Оно не
	// заменяет RTT готового туннеля и не используется для выбора ноды.
	Connect time.Duration

	// dialer остаётся живым только у победителя: переустанавливать
	// соединение сразу после удачного замера — лишний круг по сети.
	dialer *Dialer
}

// OK сообщает, годится ли нода.
func (m Measurement) OK() bool { return m.Err == nil }

// Cost — во что обходится эта нода.
//
// Порция у всех нод одна и та же, поэтому сравнивать можно прямо время: в нём
// уже и круг до ноды, и разгон, и сама передача. Разбирать его на задержку и
// скорость незачем — человек ждёт сумму.
//
// Где порцию не мерили, остаётся задержка — как было до сих пор.
func (m Measurement) Cost() time.Duration {
	if m.Fetch <= 0 {
		return m.Latency
	}
	return m.Fetch
}

// Speed — сколько это даёт в байтах в секунду, для показа человеку.
//
// Число заниженное: в него входит круг до ноды, а порция маленькая. Для
// сравнения нод это неважно — все меряются одинаково, — но выдавать его за
// скорость канала нельзя.
func (m Measurement) Speed() float64 {
	if m.Fetch <= 0 {
		return 0
	}
	return float64(vp1.DefaultSpeedSample) / m.Fetch.Seconds()
}

// Probe измеряет одну ноду.
func Probe(ctx context.Context, node Node, key vp1.KeyPair, opts Options) Measurement {
	start := time.Now()

	dialer, err := NewDialer(node, key, opts)
	if err != nil {
		return Measurement{Node: node, Err: err}
	}

	probeCtx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()

	if err := dialer.Warmup(probeCtx); err != nil {
		rtt := dialer.Connect()
		_ = dialer.Close()
		return Measurement{Node: node, Latency: time.Since(start), Connect: rtt, Err: err}
	}

	setup := time.Since(start)
	pingCtx, pingCancel := context.WithTimeout(probeCtx, 2*time.Second)
	defer pingCancel()
	rtt, _ := dialer.pool.Ping(pingCtx)
	// Неудача дополнительного замера не отменяет успешное подключение.
	m := Measurement{Node: node, Latency: setup, RTT: rtt, Connect: dialer.Connect()}
	snapshot := m
	dialer.measurement.Store(&snapshot)
	m.dialer = dialer
	return m
}

// PingMS оставляет неизвестный отклик неизвестным. Даже очень быстрый
// успешный замер занимает хотя бы 1 мс в интерфейсе: ноль означает отсутствие.
func (m Measurement) PingMS() int64 {
	if !m.OK() || m.RTT <= 0 {
		return 0
	}
	return max(1, m.RTT.Milliseconds())
}

// SelectBest меряет ноды и возвращает дозвон до самой быстрой живой.
//
// Возвращаются все замеры, а не только победитель: их надо отправить панели,
// иначе продавец так и не узнает, что половина его нод не работает у людей.
func SelectBest(ctx context.Context, nodes []Node, key vp1.KeyPair, opts Options) (*Dialer, []Measurement, error) {
	return SelectPreferred(ctx, nodes, key, opts, 0)
}

// SelectPreferred подключает выбранную человеком ноду без ожидания остальных.
//
// prefer — её идентификатор; ноль означает автовыбор. Экран выбора страны
// меряет все ноды отдельно по запросу. Повторять этот замер при подключении
// означало ждать даже те серверы, которыми человек пользоваться не собирается.
//
// Если выбранная нода не ответила, берём лучшую живую. Человек хотел
// определённую страну, но интернет он хотел сильнее; о подмене ему скажут —
// имя ноды на экране живое.
func SelectPreferred(ctx context.Context, nodes []Node, key vp1.KeyPair, opts Options, prefer int64) (*Dialer, []Measurement, error) {
	if len(nodes) == 0 {
		return nil, nil, errors.New("список нод пуст")
	}

	var results []Measurement
	if prefer != 0 {
		for index, node := range nodes {
			if node.ID != prefer {
				continue
			}
			chosen := Probe(ctx, node, key, opts)
			if chosen.OK() {
				return chosen.dialer, []Measurement{chosen}, nil
			}
			// При отказе выбранной ноды не пробуем её повторно. Отчёт об
			// отказе остаётся на её месте, чтобы панель не получила чужой ID.
			others := make([]Node, 0, len(nodes)-1)
			others = append(others, nodes[:index]...)
			others = append(others, nodes[index+1:]...)
			fallback := probeAll(ctx, others, key, opts)
			results = make([]Measurement, 0, len(nodes))
			results = append(results, fallback[:index]...)
			results = append(results, chosen)
			results = append(results, fallback[index:]...)
			break
		}
	}
	if results == nil {
		results = probeAll(ctx, nodes, key, opts)
	}

	// Сортируем копию: порядок замеров должен совпадать с порядком нод,
	// иначе отчёт панели уедет не про те ноды.
	ranked := make([]Measurement, len(results))
	copy(ranked, results)
	sort.SliceStable(ranked, func(a, b int) bool {
		if ranked[a].OK() != ranked[b].OK() {
			return ranked[a].OK()
		}
		return ranked[a].Cost() < ranked[b].Cost()
	})

	winner := ranked[0]

	if !winner.OK() {
		closeAll(results, nil)
		return nil, results, fmt.Errorf("ни одна нода не ответила: %w", winner.Err)
	}

	closeAll(results, winner.dialer)
	return winner.dialer, results, nil
}

// probeAll меряет все ноды разом и оставляет их соединения открытыми.
//
// Закрывает их тот, кто звал: победителя надо сохранить, а при простом замере
// для экрана выбора — закрыть все до одного.
func probeAll(ctx context.Context, nodes []Node, key vp1.KeyPair, opts Options) []Measurement {
	results := make([]Measurement, len(nodes))
	slots := make(chan struct{}, maxParallelProbes)
	var wg sync.WaitGroup

	for i, node := range nodes {
		wg.Add(1)
		go func(i int, node Node) {
			defer wg.Done()

			slots <- struct{}{}
			defer func() { <-slots }()

			results[i] = Probe(ctx, node, key, opts)
		}(i, node)
	}
	wg.Wait()

	measureSpeeds(ctx, results)
	return results
}

// measureAround меряет ноды, не открывая нового соединения к той, через
// которую уже идёт туннель.
//
// Раньше кнопка «Обновить» звонила и текущей ноде: к двум сессиям туннеля
// добавлялось третье рукопожатие, а второе нажатие подряд давало четвёртое.
// Это ровно тот всплеск к одному адресу, за который ТСПУ замораживает его
// (см. transport/budget.go), — и замерзал как раз рабочий туннель. Текущую
// ноду незачем мерить заново: отклик у неё есть по живому соединению.
func measureAround(ctx context.Context, nodes []Node, current *Dialer, key vp1.KeyPair, opts Options) []Measurement {
	if current == nil || !current.pool.Live() {
		return MeasureAll(ctx, nodes, key, opts)
	}
	cur := current.Node()
	others := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		if !sameNode(n, cur) {
			others = append(others, n)
		}
	}
	measured := MeasureAll(ctx, others, key, opts)

	out := make([]Measurement, 0, len(nodes))
	next := 0
	for _, n := range nodes {
		if sameNode(n, cur) {
			out = append(out, current.live(ctx))
			continue
		}
		out = append(out, measured[next])
		next++
	}
	return out
}

// sameNode — та же ли нода. ID есть не у всех подписок: у чужих и старых он
// нулевой, и тогда ноду узнаём по адресу и ключу.
func sameNode(a, b Node) bool {
	if a.ID != 0 || b.ID != 0 {
		return a.ID == b.ID
	}
	return a.Address == b.Address && a.PublicKey == b.PublicKey
}

// live — замер текущей ноды по уже открытому туннелю: прежние время
// подключения и скорость плюс свежий отклик. Неудачный отклик не делает
// ноду мёртвой — за этим следит сторож, — он только скрывает число.
func (d *Dialer) live(ctx context.Context) Measurement {
	m := Measurement{Node: d.Node(), Connect: d.Connect()}
	if prev := d.measurement.Load(); prev != nil {
		m = *prev
		m.dialer = nil
		m.Err = nil
	}
	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rtt, err := d.pool.Ping(pingCtx)
	if err != nil {
		rtt = 0
	}
	m.RTT = rtt
	return m
}

// MeasureAll меряет все ноды и ничего не оставляет открытым.
//
// Нужен экрану выбора страны: человек смотрит, где быстрее, и выбирает сам.
// Меряем с его устройства, а не берём числа у панели — панель стоит за
// границей и видит ноду живой ровно тогда, когда для телефона она уже мертва.
func MeasureAll(ctx context.Context, nodes []Node, key vp1.KeyPair, opts Options) []Measurement {
	results := probeAll(ctx, nodes, key, opts)
	closeAll(results, nil)
	return results
}

// closeAll закрывает все соединения, кроме соединения победителя.
func closeAll(list []Measurement, keep *Dialer) {
	for _, m := range list {
		if m.dialer != nil && m.dialer != keep {
			_ = m.dialer.Close()
		}
	}
}

// Warmup доводит соединение до готовности, ничего не передавая.
//
// Открытие потока протаскивает через всё: внешний слой, хендшейк VP1 и
// мультиплексор. Именно это и есть «нода работает», в отличие от «порт
// отвечает».
func (d *Dialer) Warmup(ctx context.Context) error {
	stream, err := d.pool.Open(ctx)
	if err != nil {
		return err
	}
	return stream.Close()
}

// measureSpeeds доспрашивает у живых нод, с какой скоростью они отдают данные.
//
// Только когда живых больше одной. Смысл замера — выбрать, а выбирать не из
// чего: единственную ноду мы возьмём в любом случае, и тратить на неё трафик
// продавца незачем.
//
// Неудача замера не выбрасывает ноду. Старая нода такого не умеет и закроет
// поток — это не повод считать её мёртвой: она только что ответила на
// хендшейк. Останется без скорости, и сравнится по задержке, как раньше.
func measureSpeeds(ctx context.Context, results []Measurement) {
	alive := 0
	for _, m := range results {
		if m.OK() && m.dialer != nil {
			alive++
		}
	}
	if alive < 2 {
		return
	}

	var wg sync.WaitGroup
	for i := range results {
		if !results[i].OK() || results[i].dialer == nil {
			continue
		}

		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			speedCtx, cancel := context.WithTimeout(ctx, SpeedTimeout)
			defer cancel()

			fetch, err := results[i].dialer.MeasureFetch(speedCtx, vp1.DefaultSpeedSample)
			if err != nil {
				return
			}
			results[i].Fetch = fetch
		}(i)
	}
	wg.Wait()
}
