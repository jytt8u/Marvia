package client

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/jytt8u/marvia/internal/vp1"
)

// ErrPanel помечает неудачу, случившуюся до нод: до панели не достучались.
//
// Вид неудачи важен интерфейсу. «Нет связи с панелью» и «ни одна нода не
// отвечает» — разные беды: в первом случае человеку чаще всего надо просто
// проверить интернет, во втором — писать продавцу.
var ErrPanel = errors.New("панель недоступна")

// ErrExpired и ErrQuota — подписка кончилась.
//
// Отдельно от остальных, потому что это самая частая беда и единственная, где
// человеку надо не чинить, а заплатить. Нода про это знает, но сказать не
// может: она отказывает молча, иначе по её ответам перебирали бы чужие ключи.
// Зато знает панель — и знает заранее, до первого гудка.
var (
	ErrExpired = errors.New("подписка кончилась")
	ErrQuota   = errors.New("кончился трафик по подписке")
)

// ConnectConfig — всё, что нужно, чтобы поднять дозвон до лучшей ноды.
type ConnectConfig struct {
	// Account — разобранная ссылка доступа.
	Account Account

	// Key — ключевая пара покупателя.
	Key vp1.KeyPair

	// Dial — настройки дозвона. В бою пустые.
	Dial Options

	// CachePath — файл кэша подписки. Пусто означает работу без кэша: тогда
	// последовательность ровно та же, что была до его появления.
	CachePath string

	// Log — необязательный журнал хода подключения.
	Log func(format string, args ...any)

	// Prefer — нода, выбранная человеком руками; ноль означает автовыбор.
	//
	// Идентификатор, а не имя: продавец переименовывает ноды, и выбор,
	// записанный именем, молча перестал бы действовать после переименования.
	Prefer int64

	// Временное исключение после подтверждённого зависания; в подписку и
	// настройки пользователя не записывается и снимается при смене сети.
	excluded map[string]time.Time
}

// Connect выбирает лучшую ноду, стараясь не ходить в панель.
//
// Порядок такой:
//
//  1. кэш с действующей подпиской — пробуем известные ноды сразу, даже если
//     список старше суток; протухший список обновляем в фоне;
//  2. кэша нет, подписка в нём кончилась или все ноды молчат — идём в панель
//     за свежим списком и пробуем новые адреса.
//
// Замеры возвращаются всегда, в том числе вместе с ошибкой: их ждёт панель,
// и именно про случай «не работает ничего» продавцу важнее всего узнать.
func Connect(ctx context.Context, cfg ConnectConfig) (*Dialer, []Measurement, error) {
	logf := cfg.Log
	if logf == nil {
		logf = func(string, ...any) {}
	}

	var cached CachedSubscription
	if cfg.CachePath != "" {
		var err error
		cached, err = LoadCache(cfg.CachePath, cfg.Account.SubscriptionURL)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			// Кэш — ускорение, а не условие работы. Испорченный файл
			// перезапишется следующим удачным походом в панель.
			logf("кэш подписки не прочитан: %v", err)
		}
	}

	// Замеры последней попытки. Держим их отдельно, чтобы отдать отчёт даже
	// когда все попытки провалились.
	var measurements []Measurement

	// Панель иногда отвечает только после полного HTTP-тайм-аута. Если в
	// протухшем кэше есть живая нода, ждать панель перед подключением незачем:
	// нода уже умеет проверить доступ, а обновление списка не должно держать
	// человека на экране «Подключаюсь». Кэш с датой из будущего не используем.
	cacheAge := time.Since(cached.FetchedAt)
	cacheUsable := len(cached.Nodes()) > 0 && !cached.FetchedAt.IsZero() &&
		cacheAge >= 0 && cached.Subscription.Allows() == nil
	var refresh <-chan subscriptionResult
	if cacheUsable && !cached.Fresh() && cfg.CachePath != "" {
		// Если адреса уже сменились, запрос панели идёт одновременно с
		// проверкой старых: не складываем два тайм-аута подряд.
		refresh = refreshCache(cfg)
	}
	if cacheUsable {
		logf("пробую ноды из кэша, их %d", len(cached.Nodes()))
		dialer, m, err := SelectPreferred(ctx, eligibleNodes(cached.Nodes(), cfg.excluded), cfg.Key, cfg.Dial, cfg.Prefer)
		if err == nil {
			// До обновления показываем последнюю известную подписку.
			return dialer.withSubscription(cached.Subscription), m, nil
		}
		measurements = m
		if ctx.Err() != nil {
			// Человек нажал «отключиться», не дождавшись. Тревожить панель
			// незачем: он уже ушёл.
			return nil, measurements, err
		}
		logf("ни одна нода из кэша не ответила, иду в панель")
	}

	logf("забираю список нод")
	var sub Subscription
	var err error
	if refresh != nil {
		select {
		case result := <-refresh:
			sub, err = result.sub, result.err
		case <-ctx.Done():
			return nil, measurements, ctx.Err()
		}
	} else {
		sub, err = FetchSubscription(ctx, cfg.Account.SubscriptionURL, cfg.Account.PanelIPs)
	}
	if err != nil {
		return nil, measurements, fmt.Errorf("%w: %w", ErrPanel, err)
	}

	if refresh == nil && cfg.CachePath != "" {
		if err := SaveCache(cfg.CachePath, cfg.Account.SubscriptionURL, sub); err != nil {
			// Не сохранился кэш — не повод не подключаться. В худшем случае
			// в следующий раз сходим в панель, как раньше.
			logf("кэш подписки не сохранён: %v", err)
		}
	}

	// Кончившуюся подписку видно здесь, и звонить нодам уже незачем: они
	// откажут, а человек прочитает «серверы не отвечают» и пойдёт к продавцу
	// чинить то, что не сломано.
	if err := sub.Allows(); err != nil {
		return nil, measurements, err
	}

	logf("замеряю ноды, их %d", len(sub.Nodes))
	dialer, m, err := SelectPreferred(ctx, eligibleNodes(sub.Nodes, cfg.excluded), cfg.Key, cfg.Dial, cfg.Prefer)
	return dialer.withSubscription(sub), m, err
}

func eligibleNodes(nodes []Node, excluded map[string]time.Time) []Node {
	if len(excluded) == 0 {
		return nodes
	}
	result := make([]Node, 0, len(nodes))
	for _, node := range nodes {
		if until := excluded[node.Address]; !time.Now().Before(until) {
			result = append(result, node)
		}
	}
	return result
}

type subscriptionResult struct {
	sub Subscription
	err error
}

// refreshCache обновляет вчерашний список параллельно с дозвоном к известной
// ноде. Если она не ответит, Connect дождётся уже идущего запроса вместо
// второго, последовательного похода в панель.
func refreshCache(cfg ConnectConfig) <-chan subscriptionResult {
	ch := make(chan subscriptionResult, 1)
	go func() {
		sub, err := FetchSubscription(context.Background(), cfg.Account.SubscriptionURL, cfg.Account.PanelIPs)
		if err == nil {
			_ = SaveCache(cfg.CachePath, cfg.Account.SubscriptionURL, sub)
		}
		ch <- subscriptionResult{sub: sub, err: err}
	}()
	return ch
}
