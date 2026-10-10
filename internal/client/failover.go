package client

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

// Переезд между нодами на живом туннеле.
//
// Ноду выбирали один раз — при подключении, — и дальше держались за неё, что
// бы с ней ни случилось. Проверка на живом показала, чем это оборачивается:
// заблокированную ноду пакеты не отвергают, а роняют молча, поэтому туннель не
// рвётся, а глохнет. Приложение продолжает писать «Подключено», видео доигрывает
// из буфера, а всё остальное просто не грузится — комментарии, картинки, любой
// новый запрос. Человек не понимает, что сломалось, и винит телефон, оператора,
// сайт: кого угодно, кроме нас.
//
// Обещание при этом было прямо противоположное: «ноду заблокировали, покупатель
// не заметил». Supervisor его выполняет — сам замечает, что нода замолчала, сам
// выбирает другую и подменяет дозвон под мостом.

// Значения — переменные, а не константы, ради тестов: проверка переезда с
// настоящей полуминутой шла бы дольше, чем её кто-нибудь согласится ждать, а
// проверку, которую не ждут, перестают запускать.
var (
	// Как часто щупаем текущую ноду.
	//
	// Полминуты — предел того, сколько человек согласен смотреть на
	// наполовину работающий интернет, не начав искать виноватого.
	probeEvery = 30 * time.Second

	// Сколько ждём ответа. Живая нода отвечает за доли секунды. Раньше ждали
	// восемь секунд, и вместе с keepalive замороженное соединение замечали за
	// минуту: ТСПУ держит TCP открытым и молча роняет данные после 15–20 КБ.
	// Три секунды и два промаха подряд — шесть секунд на подтверждение, а
	// медленный, но рабочий TCP от ложного переезда защищают поступающие
	// данные (Pool.Flowing), а не длинный тайм-аут.
	probeTimeout = 3 * time.Second

	// Через сколько повторяем проверку, когда уже есть основания думать, что
	// нода мертва: первый промах или сигнал от упавших соединений.
	//
	// Две секунды — это время, за которое отказ соединения успевает случиться
	// и повториться, и при этом человек ещё не успевает решить, что купил
	// нерабочее.
	probeSoon = 2 * time.Second

	// Сколько дозвонов подряд должны не дойти до ноды, чтобы будить сторожа
	// раньше расписания.
	//
	// Три, а не один: телефон переключается между вышками и между Wi-Fi и
	// сотовой сетью по нескольку раз за поездку, и каждый такой переход даёт
	// пачку мгновенных отказов, после которых всё продолжает работать.
	troubleDials = int32(3)

	// Сколько проверок подряд должны провалиться, прежде чем переезжать.
	//
	// Одиночный промах — это моргнувший мобильный интернет, и переезжать по
	// нему значит гонять человека между нодами на каждой поездке в метро.
	probeMisses = 2

	// Сколько байт просим на проверку: килобайт раз в полминуты.
	probeSample = 1024

	// Молчание при ожидающем чтении или записи лишь запускает проверку:
	// сервер сайта может отвечать долго, хотя сама нода работает.
	trafficSilence    = 5 * time.Second
	trafficCheckEvery = time.Second

	// Сколько замолчавший адрес не предлагаем при автоматическом переезде.
	// Без карантина новый хендшейк проходил до порога, выбор признавал ноду
	// живой и возвращал к ней же — и большая загрузка снова замерзала. Минута:
	// дольше держать нельзя, адрес мог замолчать из-за сети, а не блокировки.
	// Человек, выбравший ноду руками, карантин снимает.
	quarantineFor = time.Minute

	// Бюджет подключения включает панель и замеры кандидатов.
	moveTimeout = 80 * time.Second

	// Сколько ждём между неудачными попытками переезда.
	//
	// Если не отвечает ни одна нода — дело не в ноде, а в сети у человека.
	// Долбить панель и все ноды подряд в этом случае незачем.
	retryAfter = 20 * time.Second
)

// Events — то, о чём надзор сообщает наружу.
//
// Оба обработчика необязательны и оба зовутся из сторожевой горутины, поэтому
// внутри нельзя ни блокироваться надолго, ни трогать туннель: своё состояние
// поправить и выйти.
type Events struct {
	// OnSwitch — переехали на другую ноду. Окну надо переписать имя: иначе
	// оно будет показывать ту, через которую трафик давно не идёт.
	OnSwitch func(Node)

	// OnTrouble — нода замолчала, а переехать не вышло: не ответила ни одна.
	//
	// Это единственный случай, когда человеку надо сказать. Удачный переезд
	// он замечать не должен — в этом и смысл, — а вот «сейчас не работает
	// ничего» лучше прочитать у нас, чем выяснять самому.
	//
	// Приезжает код, а не готовая фраза: ядро не знает, на каком языке говорит
	// приложение. Раньше отсюда уезжало русское предложение, и английский
	// интерфейс показывал его дословно — на ветке, которая называлась
	// «клиенты говорят на двух языках».
	OnTrouble func(code string)

	// OnRecovered — нода, которую уже объявили молчащей, снова отвечает.
	//
	// Без этого события из состояния «связь потеряна» нет выхода, кроме
	// переезда. А самый обычный случай — не мёртвая нода, а пропавшая сеть:
	// метро, самолёт, перезагруженный роутер. Тогда не отвечает ни нода, ни
	// панель, переехать некуда, и надпись «связь потеряна» остаётся навсегда —
	// в том числе после того, как сеть вернулась и всё снова работает.
	OnRecovered func()
}

// Коды бед. Фразу под код подбирает приложение на своём языке.
const (
	// TroubleNoNode — текущая нода молчит, и ни одна другая не ответила.
	TroubleNoNode = "no-node"
)

// Supervisor — дозвон, который сам меняет ноду, когда текущая замолчала.
//
// Подставляется мосту вместо *Dialer: методы те же, а внутри живёт текущий
// дозвон, который можно заменить, не трогая ни мост, ни интерфейс.
type Supervisor struct {
	mu       sync.Mutex
	dialer   *Dialer
	closed   bool
	excluded map[string]time.Time

	cfg ConnectConfig
	log func(string, ...any)

	events Events

	// suspect — «трафик уже не идёт, проверь ноду немедленно».
	//
	// Сигнал приходит из настоящих соединений, а не из расписания. Ёмкости в
	// одну штуку достаточно: сторожу неважно, сколько соединений упало, важно
	// только, что упало хоть одно.
	suspect chan struct{}

	// Смена сети отменяет работу на старом маршруте, включая долгий переезд.
	life          context.Context
	networkCtx    context.Context
	networkCancel context.CancelFunc
	networkWake   chan struct{}
	offline       bool

	// fails — сколько дозвонов подряд не дошли до ноды.
	fails atomic.Int32

	cancel context.CancelFunc
	done   chan struct{}
}

// Supervise подключается и заводит сторожа.
//
// Первое подключение ничем не отличается от прежнего Connect: те же кэш,
// подписка и замеры. Разница начинается после — с этой минуты за нодой следят.
func Supervise(ctx context.Context, cfg ConnectConfig, events Events) (*Supervisor, []Measurement, error) {
	dialer, m, err := Connect(ctx, cfg)
	if err != nil {
		return nil, m, err
	}

	logf := cfg.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	// Сторож живёт своей жизнью, а не жизнью вызова: ctx у Connect кончается
	// вместе с подключением, а следить надо всё время, пока стоит туннель.
	watchCtx, cancel := context.WithCancel(context.Background())
	networkCtx, networkCancel := context.WithCancel(watchCtx)

	s := &Supervisor{
		dialer:        dialer,
		cfg:           cfg,
		log:           logf,
		events:        events,
		suspect:       make(chan struct{}, 1),
		cancel:        cancel,
		done:          make(chan struct{}),
		life:          watchCtx,
		networkCtx:    networkCtx,
		networkCancel: networkCancel,
		networkWake:   make(chan struct{}, 1),
	}

	go s.watch(watchCtx)
	return s, m, nil
}

// DialTarget открывает поток до цели через ту ноду, которая сейчас выбрана.
func (s *Supervisor) DialTarget(ctx context.Context, target vp1.Address) (net.Conn, error) {
	d, err := s.current()
	if err != nil {
		return nil, err
	}
	conn, err := d.DialTarget(ctx, target)
	s.noticed(ctx, err)
	// Ответ ноды теперь приходит позже, первым чтением (early.go). Смерть
	// ноды, обнаруженная там, должна будить сторожа так же, как раньше.
	if early, ok := conn.(interface{ OnStatus(func(error)) }); ok {
		early.OnStatus(func(err error) { s.noticed(context.Background(), err) })
	}
	return conn, err
}

// DialDatagrams — то же для датаграмм.
func (s *Supervisor) DialDatagrams(ctx context.Context, target vp1.Address) (net.Conn, error) {
	d, err := s.current()
	if err != nil {
		return nil, err
	}
	conn, err := d.DialDatagrams(ctx, target)
	s.noticed(ctx, err)
	return conn, err
}

// noticed считает дозвоны, не дошедшие до ноды, и будит сторожа.
//
// Это самое дешёвое знание о смерти ноды, какое есть: настоящие соединения
// идут десятками в секунду и упираются в отказ мгновенно, тогда как плановая
// проверка узнаёт то же самое в среднем через полминуты.
//
// Раньше этот сигнал выбрасывался. На живой проверке ноду остановили, на
// экране телефона пошли отказы дозвона — и за минуту наблюдения переезда так и
// не случилось: помогло только переподключение руками. По расписанию его и не
// могло быть, полминуты между проверками и две проверки подряд дают от сорока
// до восьмидесяти секунд слепоты, в которые не работает ничего.
//
// Считаем только недоступность самой ноды. Отказ ноды по конкретному адресу —
// это закрытый сайт, а не мёртвая нода, и переезжать из-за него нельзя.
func (s *Supervisor) noticed(ctx context.Context, err error) {
	if err == nil {
		s.fails.Store(0)
		return
	}

	// Отменённый вызов — не смерть ноды.
	//
	// Мост сворачивает поток, когда приложение закрыло соединение, и это
	// обычное дело: вкладку закрыли, ролик долистали. Считать такое отказом
	// значит менять ноду каждый раз, когда человек листает ленту.
	if ctx.Err() != nil {
		return
	}
	if !errors.Is(err, vp1.ErrNodeUnreachable) {
		return
	}

	// Одного отказа мало: мобильная сеть моргает на каждом переходе между
	// вышками, и будить сторожа на каждое такое моргание значит гонять
	// человека между нодами всю дорогу до работы.
	if s.fails.Add(1) < troubleDials {
		return
	}
	s.fails.Store(0)

	select {
	case s.suspect <- struct{}{}:
	default:
		// Сторож уже разбужен и ещё не дошёл до проверки. Второй сигнал ему
		// ничего не добавит.
	}
}

// Select переводит туннель на ноду, выбранную человеком.
//
// id == 0 означает возврат к автовыбору. Выбор запоминается и переживает
// переезды: если выбранная страна замолчала, надзор уедет на живую, а когда
// человек переподключится — вернётся к выбранной, если она ожила.
//
// Туннель при этом не рвётся: меняется дозвон под мостом, ровно как при
// переезде. Человек не теряет ни одного открытого соединения, кроме тех, что
// шли через прежнюю ноду, — их не сохранить в принципе.
func (s *Supervisor) Select(ctx context.Context, id int64) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("туннель закрыт")
	}
	s.cfg.Prefer = id
	cur := s.dialer
	// Явный выбор разрешает повторить адрес; автоматический надзор этого
	// не делает, пока действует временное исключение.
	if cur != nil {
		for _, node := range cur.Subscription().Nodes {
			if node.ID == id {
				delete(s.excluded, node.Address)
			}
		}
	}
	s.mu.Unlock()

	// Уже на ней — переподключаться незачем: это стоило бы человеку всех
	// открытых соединений ради того, что и так выполнено.
	if cur != nil && id != 0 && cur.Node().ID == id && cur.pool.Live() {
		return nil
	}

	if !s.move(ctx, cur) {
		return errors.New("не вышло переключиться: нода не ответила")
	}
	return nil
}

// Selected — что выбрано руками. Ноль означает автовыбор.
func (s *Supervisor) Selected() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Prefer
}

// Nodes — список нод из последней подписки.
func (s *Supervisor) Nodes() []Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialer == nil {
		return nil
	}
	return s.dialer.Subscription().Nodes
}

// Measure меряет все ноды заново — для экрана выбора страны.
//
// Отдельными соединениями, не через туннель: замер должен показать, как
// откроется нода, а не как она работает через другую ноду.
func (s *Supervisor) Measure(ctx context.Context) []Measurement {
	s.mu.Lock()
	if s.closed || s.offline || s.dialer == nil {
		s.mu.Unlock()
		return nil
	}
	cfg := s.cfg
	d := s.dialer
	nodes := d.Subscription().Nodes
	networkCtx := s.networkCtx
	s.mu.Unlock()

	if len(nodes) == 0 {
		return nil
	}
	measureCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if networkCtx != nil {
		stop := context.AfterFunc(networkCtx, cancel)
		defer stop()
	}
	results := measureAround(measureCtx, nodes, d, cfg.Key, cfg.Dial)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Даже тот же номер ноды уже относится к другому маршруту после смены
	// сети. Поздний результат не должен возвращать старый пинг в интерфейс.
	if s.closed || s.offline || s.dialer != d || measureCtx.Err() != nil {
		return nil
	}
	for _, m := range results {
		if m.Node.ID == d.Node().ID {
			m.dialer = nil
			d.measurement.Store(&m)
			break
		}
	}
	return results
}

// Ping меряет отклик текущей ноды внутри готового туннеля и запоминает его.
//
// Это один запрос-ответ мультиплексора по уже открытой сессии: ни нового
// хендшейка, ни нового соединения. Поэтому его можно звать раз в несколько
// секунд, пока человек смотрит на число, — в отличие от Measure, которая
// открывает соединение к каждой ноде.
func (s *Supervisor) Ping(ctx context.Context) (time.Duration, error) {
	s.mu.Lock()
	d := s.dialer
	s.mu.Unlock()
	if d == nil {
		return 0, errors.New("туннель не поднят")
	}
	rtt, err := d.pool.Ping(ctx)
	if err != nil {
		d.recordRTT(0)
		return 0, err
	}
	d.recordRTT(rtt)
	return rtt, nil
}

// Measurement возвращает замер подключения текущей ноды. После переезда
// нельзя продолжать показывать отклик предыдущего сервера.
func (s *Supervisor) Measurement() Measurement {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialer == nil {
		return Measurement{}
	}
	if m := s.dialer.measurement.Load(); m != nil {
		return *m
	}
	return Measurement{}
}

// Node — нода, через которую идёт трафик прямо сейчас.
func (s *Supervisor) Node() Node {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialer == nil {
		return Node{}
	}
	return s.dialer.Node()
}

// Subscription — срок и остаток квоты.
func (s *Supervisor) Subscription() Subscription {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialer == nil {
		return Subscription{}
	}
	return s.dialer.Subscription()
}

// Close гасит сторожа и закрывает текущий дозвон.
func (s *Supervisor) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	d := s.dialer
	s.dialer = nil
	s.mu.Unlock()

	s.cancel()
	<-s.done

	if d != nil {
		return d.Close()
	}
	return nil
}

func (s *Supervisor) current() (*Dialer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dialer == nil {
		return nil, errors.New("туннель закрыт")
	}
	if s.offline {
		return nil, fmt.Errorf("нет внешней сети: %w", vp1.ErrNodeUnreachable)
	}
	return s.dialer, nil
}

// watch щупает текущую ноду и переезжает, когда она перестаёт отвечать.
func (s *Supervisor) watch(ctx context.Context) {
	defer close(s.done)

	// Таймер, а не тикер: после сигнала от трафика следующая проверка должна
	// случиться скоро, а не по прежнему получасовому расписанию.
	timer := time.NewTimer(probeEvery)
	defer timer.Stop()
	traffic := time.NewTicker(trafficCheckEvery)
	defer traffic.Stop()

	misses := 0

	// byWarmup — эта нода не умеет замер, следим за ней прогревом.
	byWarmup := false

	// troubled — человеку уже сказали, что связи нет. Надо будет сказать и
	// обратное, когда она вернётся.
	troubled := false
	var checked *Dialer
	quarantined := false
	var retryAt time.Time
	for {
		networkChanged := false
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-traffic.C:
			s.mu.Lock()
			d, offline := s.dialer, s.offline
			s.mu.Unlock()
			if offline || d == nil || !d.pool.Stalled(trafficSilence) {
				continue
			}
			stopTimer(timer)
		case <-s.networkWake:
			networkChanged = true
		case <-s.suspect:
			if time.Now().Before(retryAt) {
				continue
			}
			stopTimer(timer)
		}
		if networkChanged {
			stopTimer(timer)
			misses = 0
			byWarmup = false
			troubled = true
		}
		s.mu.Lock()
		networkCtx := s.networkCtx
		offline := s.offline
		d := s.dialer
		s.mu.Unlock()
		if networkCtx == nil {
			networkCtx = ctx
		}
		if offline {
			timer.Reset(probeEvery)
			continue
		}

		if d == nil {
			return
		}
		if d != checked {
			misses, byWarmup = 0, false
			checked = d
			quarantined = false
			retryAt = time.Time{}
		}
		legacy, err := checkConnection(networkCtx, d, networkChanged, byWarmup)
		if ctx.Err() != nil {
			return
		}
		if networkCtx.Err() != nil {
			continue
		}
		if legacy && !byWarmup {
			s.log("нода %s не умеет замер — старая версия; слежу за ней проще", d.Node().Title())
		}
		byWarmup = legacy

		if err == nil {
			misses = 0

			// Сеть вернулась. Сказать об этом обязательно: надпись «связь
			// потеряна» сама по себе не гаснет, и человек будет смотреть на
			// неё, пока не переподключится руками, — хотя всё уже работает.
			if troubled {
				troubled = false
				s.log("нода %s снова отвечает", d.Node().Title())
				if s.events.OnRecovered != nil {
					s.events.OnRecovered()
				}
			}

			timer.Reset(probeEvery)
			continue
		}

		d.recordRTT(0)
		misses++
		if misses < probeMisses {
			// Промах уже был: второй проверки ждём секунды, а не полминуты.
			// Полминуты между двумя промахами — это полминуты, которые человек
			// смотрит на неработающий интернет ради подтверждения того, что
			// первая проверка и так показала.
			timer.Reset(probeSoon)
			continue
		}

		s.log("нода %s не отвечает на %d проверки подряд: %v", d.Node().Title(), misses, err)
		s.mu.Lock()
		if s.dialer != d || networkCtx.Err() != nil {
			s.mu.Unlock()
			continue
		}
		if s.excluded == nil {
			s.excluded = make(map[string]time.Time)
		}
		if !quarantined {
			s.excluded[d.Node().Address] = time.Now().Add(quarantineFor)
		}
		s.mu.Unlock()
		if !quarantined {
			d.pool.Quarantine()
			quarantined = true
		}
		pick, cancelPick := context.WithTimeout(networkCtx, 15*time.Second)
		moved := s.move(pick, d)
		cancelPick()
		if moved {
			misses = 0
			s.fails.Store(0)
			// Вывод «нода старая» относился к прежней ноде. Новую спрашиваем
			// полным замером, пока она сама не покажет обратное.
			byWarmup = false
			timer.Reset(probeEvery)
			continue
		}
		if networkCtx.Err() != nil {
			continue
		}

		// Переехать не вышло — молчать об этом нельзя. Удачный переезд человек
		// замечать не должен, а «не отвечает ни одна нода» лучше прочитать у
		// нас, чем гадать, почему интернет наполовину.
		//
		// Только не в момент закрытия: человек нажал «отключиться», а надзор
		// в последний миг успевает поставить ему «нода не отвечает».
		troubled = true
		if s.events.OnTrouble != nil && ctx.Err() == nil {
			s.events.OnTrouble(TroubleNoNode)
		}

		// Переехать не вышло — подождём и попробуем снова. Счётчик не
		// сбрасываем: следующая же неудачная проверка снова приведёт сюда.
		retryAt = time.Now().Add(retryAfter)
		timer.Reset(retryAfter)
	}
}

// checkConnection отличает отказ ноды от очереди медленного TCP и отсутствия
// замера в старой версии. Смена сети требует нового рукопожатия.
//
// Сначала пинг каждой уже открытой сессии, и только потом замер: свежий
// хендшейк проходит и до замороженного порога, поэтому проверка новой сессией
// показывала «жива» ноде, на которой стоят все текущие загрузки. Замер тем же
// путём, каким выбирали ноду, — сессия, поток, ответ, — ловит и ноду, которая
// жива, а обслуживать перестала.
func checkConnection(ctx context.Context, d *Dialer, networkChanged, byWarmup bool) (bool, error) {
	timeout := probeTimeout
	if networkChanged {
		// Бюджет нового соединения включает отказ UDP и переход на TCP.
		timeout = ProbeTimeout
	}
	probe, cancel := context.WithTimeout(ctx, timeout)
	var err error
	measurementFailed := false
	if networkChanged {
		err = d.Warmup(probe)
		if err == nil {
			if rtt, pingErr := d.pool.Ping(probe); pingErr == nil {
				d.recordRTT(rtt)
			}
		}
	} else {
		err = d.pool.Check(probe)
		if err == nil && !byWarmup {
			_, err = d.MeasureFetch(probe, probeSample)
			measurementFailed = err != nil
		}
	}
	cancel()
	if ctx.Err() != nil {
		return byWarmup, ctx.Err()
	}

	// Контрольный ответ может ждать за загрузкой. Поступающие данные
	// подтверждают работу ноды, даже если короткая проверка не успела.
	if err != nil && d.pool.Flowing(trafficSilence) {
		return byWarmup, nil
	}
	if !measurementFailed || !closedEarly(err) || byWarmup || networkChanged {
		return byWarmup, err
	}

	// Замер не умеет старая нода: она видит незнакомый вид запроса и
	// закрывает поток. Ноды продавец обновляет по одной, так что нода на
	// прошлой версии — обычное состояние посреди выкатки. Считай мы её
	// мёртвой, получался бы вечный круг: переехали, выбор вернул её же как
	// живую, через полминуты всё сначала — и каждый круг рвал бы открытые
	// соединения. Поэтому спрашиваем проще: подняться до готовности нода
	// обязана в любой версии.
	//
	// Старой версией закрытие считаем, только если мультиплексор отвечает:
	// на замороженном TCP поток тоже «закрывается», и раньше это давало
	// ложное «старая версия ноды». У упрощённой проверки свой тайм-аут.
	probe, cancel = context.WithTimeout(ctx, probeTimeout)
	warmErr := d.Warmup(probe)
	cancel()
	if ctx.Err() != nil {
		return byWarmup, ctx.Err()
	}
	if warmErr == nil {
		return true, nil
	}
	return byWarmup, err
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

// move выбирает другую ноду и подменяет дозвон.
//
// Список берётся обычным путём — сначала кэш, потом панель, — то есть тем же
// Connect, что и при первом подключении. Мёртвую ноду отдельно исключать не
// надо: замер до неё не пройдёт, и SelectBest отложит её сам. Если она к тому
// же выбыла из подписки, панель её и не пришлёт.
func (s *Supervisor) move(ctx context.Context, dead *Dialer) bool {
	// На переезд даём столько же, сколько на обычное подключение: он и есть
	// обычное подключение, только человек его не нажимал.
	pick, cancel := context.WithTimeout(ctx, moveTimeout)
	defer cancel()

	// Настройки читаем под замком: выбор страны человек меняет из другого
	// потока, и без замка это была бы гонка за поле cfg.Prefer.
	s.mu.Lock()
	cfg := s.cfg
	cfg.excluded = make(map[string]time.Time, len(s.excluded))
	for address, until := range s.excluded {
		if time.Now().Before(until) {
			cfg.excluded[address] = until
		} else {
			delete(s.excluded, address)
		}
	}
	s.mu.Unlock()

	fresh, _, err := Connect(pick, cfg)
	if err != nil {
		s.log("переехать не удалось: %v", err)
		return false
	}

	if ctx.Err() != nil {
		_ = fresh.Close()
		return false
	}

	s.mu.Lock()
	if s.closed || s.dialer != dead || pick.Err() != nil {
		s.mu.Unlock()
		_ = fresh.Close()
		return false
	}
	same := s.dialer != nil && s.dialer.Node().Address == fresh.Node().Address
	s.dialer = fresh
	s.mu.Unlock()

	// Старый дозвон закрываем после подмены, а не до: между «закрыл» и
	// «поставил новый» мост остался бы без дозвона, и запросы в этот зазор
	// получили бы ошибку на ровном месте.
	if dead != nil {
		_ = dead.Close()
	}

	if same {
		// Выбор вернулся к той же ноде: значит она ожила, пока мы искали
		// замену. Соединение всё равно новое — старое-то не работало.
		s.log("нода %s ответила заново, туннель пересобран", fresh.Node().Title())
	} else {
		s.log("переехали на %s", fresh.Node().Title())
	}

	if s.events.OnSwitch != nil {
		s.events.OnSwitch(fresh.Node())
	}
	return true
}
