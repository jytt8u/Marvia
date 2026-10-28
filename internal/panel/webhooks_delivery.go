package panel

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/jytt8u/marvia/internal/egress"
)

const webhookTimeout = 15 * time.Second

type Webhooks struct {
	store  *Store
	client *http.Client
	mu     sync.Mutex
}

func NewWebhooks(store *Store) *Webhooks {
	// Proxy намеренно nil: переменная HTTPS_PROXY не должна обходить
	// проверку сети. egress.Dial проверяет все DNS-ответы и соединяется с
	// проверенным IP, сохраняя имя для TLS; повторного разрешения нет.
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, target string) (net.Conn, error) {
			return egress.Dial(ctx, network, target, webhookTimeout)
		},
		DisableKeepAlives:      true,
		TLSHandshakeTimeout:    10 * time.Second,
		ResponseHeaderTimeout:  10 * time.Second,
		MaxResponseHeaderBytes: 32 << 10,
	}
	return &Webhooks{store: store, client: &http.Client{
		Transport: transport, Timeout: webhookTimeout,
		// Перенаправление могло бы унести и подпись, и данные покупателей
		// другому хозяину. Даже переход на другой HTTPS здесь не разрешён.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

// Watch начинает работу сразу: перезапуск не прибавляет к сроку доставки
// ни секунды. Бот получает события без собственного опроса панели.
func (d *Webhooks) Watch(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var checked time.Time
	for {
		now := time.Now().UTC()
		if checked.IsZero() || now.Sub(checked) >= time.Minute {
			if err := d.Observe(ctx, now); err != nil && ctx.Err() == nil {
				log.Printf("не удалось проверить события вебхуков")
			}
			checked = now
		}
		if err := d.Deliver(ctx, now); err != nil && ctx.Err() == nil {
			log.Printf("не удалось обновить очередь вебхуков")
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Observe сохраняет переходы, а не повторяет одно состояние раз в минуту.
// В отличие от Telegram, отметки здесь долговечные: повтор webhook может
// запустить действие бота, а не просто нарисовать ещё одно сообщение.
func (d *Webhooks) Observe(ctx context.Context, now time.Time) error {
	tx, err := d.store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	cfg, err := readWebhookSettings(ctx, tx)
	if err != nil || !cfg.Ready() {
		return err
	}
	rows, err := tx.QueryContext(ctx, webhookUserSQL+` WHERE enabled = 1`)
	if err != nil {
		return err
	}
	var users []WebhookUser
	for rows.Next() {
		user, err := scanWebhookUser(rows)
		if err != nil {
			rows.Close()
			return err
		}
		users = append(users, user)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, user := range users {
		expired := user.ExpiresAt != nil && !user.ExpiresAt.After(now)
		empty := user.TrafficLimit > 0 && user.Used >= user.TrafficLimit
		left := user.TrafficLimit - user.Used
		// Умножение left на десять переполнилось бы на большой квоте. Ровно
		// десять процентов ещё не «меньше», а ноль имеет отдельное событие.
		low := user.TrafficLimit > 0 && left > 0 && (left < user.TrafficLimit/10 || (left == user.TrafficLimit/10 && user.TrafficLimit%10 > 0))
		for _, signal := range []struct {
			kind   string
			active bool
		}{{WebhookUserExpired, expired}, {WebhookTrafficEmpty, empty}, {WebhookTrafficLow, low}} {
			if err := observeWebhookSignal(ctx, tx, WebhookEvent{Event: signal.kind, At: now.UTC(), User: &user}, signal.active, ""); err != nil {
				return err
			}
		}
	}
	rows, err = tx.QueryContext(ctx, `SELECT id, name, enabled, last_seen, created_at FROM nodes`)
	if err != nil {
		return err
	}
	var nodes []Node
	for rows.Next() {
		var n Node
		var enabled int
		var seen sql.NullString
		var created string
		if err := rows.Scan(&n.ID, &n.Name, &enabled, &seen, &created); err != nil {
			rows.Close()
			return err
		}
		n.Enabled = enabled != 0
		n.CreatedAt = parse(created)
		if seen.Valid {
			at := parse(seen.String)
			n.LastSeen = &at
		}
		nodes = append(nodes, n)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if !n.Enabled {
			// Выключение продавцом не является ни аварией, ни восстановлением.
			if _, err := tx.ExecContext(ctx, `DELETE FROM webhook_state WHERE node_id = ?`, n.ID); err != nil {
				return err
			}
			continue
		}
		last := n.CreatedAt
		if n.LastSeen != nil {
			last = *n.LastSeen
		}
		silent := now.Sub(last) > silentFor
		if err := observeWebhookSignal(ctx, tx, WebhookEvent{Event: WebhookNodeDown, At: now.UTC(), Node: &WebhookNode{ID: n.ID, Name: n.Name}}, silent, WebhookNodeUp); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func observeWebhookSignal(ctx context.Context, tx *sql.Tx, event WebhookEvent, active bool, recovery string) error {
	var userID, nodeID int64
	if event.User != nil {
		userID = event.User.ID
	}
	if event.Node != nil {
		nodeID = event.Node.ID
	}
	key := fmt.Sprintf("%s:%d", event.Event, userID+nodeID)
	var previous int
	err := tx.QueryRowContext(ctx, `SELECT active FROM webhook_state WHERE key = ?`, key).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if active != (previous != 0) {
		if active || recovery != "" {
			if !active {
				event.Event = recovery
			}
			if err := enqueueWebhookTx(ctx, tx, event); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO webhook_state(key, active, user_id, node_id) VALUES (?, ?, ?, ?) ON CONFLICT(key) DO UPDATE SET active = excluded.active`, key, boolInt(active), nullID(userID), nullID(nodeID))
	return err
}

type webhookDelivery struct {
	id, endpoint, event, signature string
	body                           []byte
	deadline                       time.Time
	attempts                       int
}

// Deliver использует уже сохранённые байты и id. Даже если бот ответил, а
// панель умерла до удаления строки, повтор останется узнаваемым для бота.
func (d *Webhooks) Deliver(ctx context.Context, now time.Time) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	res, err := d.store.db.ExecContext(ctx, `DELETE FROM webhook_queue WHERE deadline <= ?`, format(now))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		d.store.Record(ctx, Event{Action: EventWebhookFailed, Detail: fmt.Sprintf("не подтверждено за сутки: %d", n)})
	}
	rows, err := d.store.db.QueryContext(ctx, `SELECT id, endpoint, event, body, signature, deadline, attempts FROM webhook_queue WHERE next_at <= ? ORDER BY next_at, id LIMIT 16`, format(now))
	if err != nil {
		return err
	}
	var jobs []webhookDelivery
	for rows.Next() {
		var job webhookDelivery
		var deadline string
		if err := rows.Scan(&job.id, &job.endpoint, &job.event, &job.body, &job.signature, &deadline, &job.attempts); err != nil {
			rows.Close()
			return err
		}
		job.deadline = parse(deadline)
		jobs = append(jobs, job)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	// Один недоступный бот не должен задерживать все остальные адреса на
	// собственный таймаут. Ограничение не даёт очереди занять сотни сокетов.
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstError error
	slots := make(chan struct{}, 4)
	for _, job := range jobs {
		if ctx.Err() != nil {
			break
		}
		slots <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			err := d.deliverOne(ctx, job, now)
			if err != nil {
				mu.Lock()
				if firstError == nil {
					firstError = err
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return firstError
}

func (d *Webhooks) deliverOne(ctx context.Context, job webhookDelivery, now time.Time) error {
	endpoint, err := checkWebhookURL(job.endpoint)
	confirmed := false
	if err == nil {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(job.body))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "Marvia-Webhooks/1")
			req.Header.Set("X-Marvia-Signature", job.signature)
			req.Header.Set("X-Marvia-Event", job.event)
			req.Header.Set("X-Marvia-Delivery", job.id)
			resp, err := d.client.Do(req)
			if err == nil {
				confirmed = resp.StatusCode >= 200 && resp.StatusCode < 300
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
				resp.Body.Close()
			}
		}
	}
	if confirmed {
		_, err = d.store.db.ExecContext(ctx, `DELETE FROM webhook_queue WHERE id = ?`, job.id)
		return err
	}
	// Ошибка HTTP содержит URL, где у продавца могут быть свои секреты.
	// Ни адрес, ни ответ бота не попадают в журнал панели.
	pause := 30 * time.Second
	for i := 0; i < job.attempts && pause < 6*time.Hour; i++ {
		pause *= 2
	}
	if pause > 6*time.Hour {
		pause = 6 * time.Hour
	}
	next := now.Add(pause)
	if next.After(job.deadline) {
		next = job.deadline
	}
	_, err = d.store.db.ExecContext(ctx, `UPDATE webhook_queue SET attempts = attempts + 1, next_at = ? WHERE id = ?`, format(next), job.id)
	return err
}
