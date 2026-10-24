package panel

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Отчёты о доступности приходят от клиентов — то есть из недоверенного
// источника, и это определяет всю конструкцию.
//
// Панель стоит за границей и видит ноду живой ровно тогда, когда для телефона
// в Иркутске она уже мертва. Узнать правду можно только у самих устройств.
// Но раз источник недоверенный, отчёт не должен быть командой: иначе один
// злонамеренный подписчик выкинет из подписки все ноды продавца, и сервис
// умрёт без всякой блокировки.
//
// Отсюда три правила, которые здесь и реализованы.
//
// Первое: отчёт от каждого подписчика хранится ровно один на ноду, последний.
// Это и ограничивает рост таблицы, и делает вес одинаковым — накрутить
// количеством сообщений нельзя.
//
// Второе: ноды по отчётам не отключаются, а только переставляются в подписке.
// Худшее, чего добьётся вредитель, — подвинет живую ноду вниз списка.
//
// Третье: ноду считаем проблемной, только когда на неё жалуется несколько
// разных подписчиков. Один человек с выключенным вайфаем — не событие. То же
// с задержкой: порядок в подписке она меняет только от нескольких человек.

const (
	// healthWindow — за какой срок учитываем отчёты. Старые не выбрасываем
	// сразу: свежая жалоба перезапишет запись того же подписчика.
	healthWindow = 6 * time.Hour

	// minReporters — сколько разных подписчиков должны пожаловаться, прежде
	// чем нода считается проблемной.
	minReporters = 3

	// throttleFetchMS — медленнее этого пробную порцию (128 КБ) нода отдаёт,
	// только когда её режут: 128 КБ за 2,5 секунды — около 400 кбит/с. Живая
	// нода отдаёт её за доли секунды даже с другого конца света, а ТСПУ,
	// замедляя адрес, опускает его до сотни-другой килобит. Порог грубый
	// нарочно: «медленно» у одного покупателя бывает от его вайфая, а
	// «режут» — это медиана нескольких.
	throttleFetchMS = 2500

	// maxFetchMS — дольше этого порцию не ждёт ни один клиент; больше в
	// базу не пишем: число приходит от покупателя и доверять ему нельзя.
	maxFetchMS = 10 * 60 * 1000
)

// Report — отчёт одного подписчика об одной ноде.
type Report struct {
	NodeID    int64 `json:"node_id"`
	OK        bool  `json:"ok"`
	LatencyMS int64 `json:"latency_ms"`

	// FetchMS — за сколько нода отдала покупателю пробную порцию; ноль — не
	// мерили. Приложения постарше поля не шлют.
	FetchMS int64 `json:"fetch_ms,omitempty"`
}

// Health — сводка по ноде.
type Health struct {
	NodeID int64 `json:"node_id"`

	// OK и Failed — сколько разных подписчиков сообщили об успехе и отказе.
	OK     int `json:"ok"`
	Failed int `json:"failed"`

	// MedianLatencyMS — медиана среди успешных замеров. Медиана, а не
	// среднее: один подписчик с очень плохой связью не должен утаскивать
	// оценку ноды вниз.
	MedianLatencyMS int64 `json:"median_latency_ms"`

	// MedianFetchMS — медиана времени пробной порции у тех, кто её мерил;
	// Fetched — сколько таких. По ним видно, что ноду режут: подключиться
	// дают, а тянуть — нет.
	MedianFetchMS int64 `json:"median_fetch_ms,omitempty"`
	Fetched       int   `json:"fetched,omitempty"`
}

// Throttled — ноду режут: несколько разных покупателей тянут с неё пробную
// порцию дольше throttleFetchMS. Отличается от Degraded: туда не пускают
// вовсе, здесь пускают, но еле-еле — и «нода на связи» в панели этого не
// показывает.
func (h Health) Throttled() bool {
	return h.Fetched >= minReporters && h.MedianFetchMS >= throttleFetchMS
}

// FetchKbps — скорость, которую видят покупатели, в килобитах в секунду.
// Заниженная (в порцию входит круг до ноды), но для «режут или нет» хватает.
func (h Health) FetchKbps() int64 {
	if h.MedianFetchMS <= 0 {
		return 0
	}
	return int64(128*8*1000) / h.MedianFetchMS
}

// Degraded сообщает, считается ли нода проблемной.
func (h Health) Degraded() bool {
	return h.Failed >= minReporters && h.Failed > h.OK
}

// Total — сколько всего подписчиков отчитались.
func (h Health) Total() int { return h.OK + h.Failed }

// SaveReports принимает отчёты одного подписчика.
func (s *Store) SaveReports(ctx context.Context, userID int64, reports []Report) error {
	if len(reports) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := format(time.Now().UTC())
	for _, r := range reports {
		// Отчёт про несуществующую ноду тихо пропускаем: подписка могла
		// устареть, а падать из-за этого незачем.
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM nodes WHERE id = ?`, r.NodeID).Scan(&exists); err != nil {
			continue
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO node_reports (node_id, user_id, ok, latency_ms, fetch_ms, reported_at)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (node_id, user_id) DO UPDATE SET
				ok = excluded.ok, latency_ms = excluded.latency_ms, fetch_ms = excluded.fetch_ms, reported_at = excluded.reported_at`,
			r.NodeID, userID, boolInt(r.OK), r.LatencyMS, min(max(r.FetchMS, 0), maxFetchMS), now); err != nil {
			return fmt.Errorf("запись отчёта: %w", err)
		}
	}
	return tx.Commit()
}

// NodeHealth собирает сводку по всем нодам.
func (s *Store) NodeHealth(ctx context.Context) (map[int64]Health, error) {
	since := format(time.Now().UTC().Add(-healthWindow))

	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id, ok, latency_ms, fetch_ms FROM node_reports
		WHERE reported_at >= ?
		ORDER BY node_id`, since)
	if err != nil {
		return nil, fmt.Errorf("чтение отчётов: %w", err)
	}
	defer rows.Close()

	latencies := map[int64][]int64{}
	fetches := map[int64][]int64{}
	out := map[int64]Health{}

	for rows.Next() {
		var (
			nodeID  int64
			okFlag  int
			latency int64
			fetch   int64
		)
		if err := rows.Scan(&nodeID, &okFlag, &latency, &fetch); err != nil {
			return nil, err
		}

		h := out[nodeID]
		h.NodeID = nodeID
		if okFlag != 0 {
			h.OK++
			if latency > 0 {
				latencies[nodeID] = append(latencies[nodeID], latency)
			}
			if fetch > 0 {
				fetches[nodeID] = append(fetches[nodeID], fetch)
			}
		} else {
			h.Failed++
		}
		out[nodeID] = h
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for nodeID, values := range latencies {
		h := out[nodeID]
		h.MedianLatencyMS = median(values)
		out[nodeID] = h
	}
	for nodeID, values := range fetches {
		h := out[nodeID]
		h.Fetched = len(values)
		h.MedianFetchMS = median(values)
		out[nodeID] = h
	}
	return out, nil
}

// median возвращает середину набора.
func median(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sort.Slice(values, func(a, b int) bool { return values[a] < values[b] })
	return values[len(values)/2]
}

// RankNodes расставляет ноды в том порядке, в каком их стоит предлагать.
//
// Сначала здоровые, потом по скорости, и только в самом конце те, на кого
// жалуются. Ни одна нода из списка не пропадает: отчёты — подсказка, а не
// команда, а источник у них недоверенный.
func RankNodes(nodes []Node, health map[int64]Health) []Node {
	ranked := make([]Node, len(nodes))
	copy(ranked, nodes)

	sort.SliceStable(ranked, func(a, b int) bool {
		ha, hb := health[ranked[a].ID], health[ranked[b].ID]

		if ha.Degraded() != hb.Degraded() {
			return hb.Degraded()
		}
		// Зарезанная — после здоровых: подключиться к ней можно, но тянуть
		// с неё нечего. Приложение всё равно меряет само; порядок помогает
		// тем, кто выбирает руками и смотрит в верх списка.
		if ha.Throttled() != hb.Throttled() {
			return hb.Throttled()
		}

		// Задержку учитываем по тому же правилу, что и жалобы: от трёх
		// разных подписчиков. Медиана из одного замера — это сам замер, и
		// без порога один вредитель поднимал бы наверх заблокированную
		// ноду, сообщив про неё «1 мс». Продавцу медиана видна и так —
		// порог только про порядок в подписке.
		ha.MedianLatencyMS, hb.MedianLatencyMS = trusted(ha), trusted(hb)

		// Нода без единого отчёта не хуже и не лучше: ей просто ещё не
		// поставили оценку, и загонять её в конец списка было бы неверно —
		// именно так новая нода никогда бы не получила первого клиента.
		switch {
		case ha.MedianLatencyMS == 0 && hb.MedianLatencyMS == 0:
			return false
		case ha.MedianLatencyMS == 0:
			return false
		case hb.MedianLatencyMS == 0:
			return true
		default:
			return ha.MedianLatencyMS < hb.MedianLatencyMS
		}
	})
	return ranked
}

// trusted — задержка ноды, если о ней сообщили достаточно разных подписчиков,
// иначе ноль, то есть «оценки нет».
func trusted(h Health) int64 {
	if h.OK < minReporters {
		return 0
	}
	return h.MedianLatencyMS
}
