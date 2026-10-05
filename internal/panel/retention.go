package panel

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Сроки хранения в одном месте.
//
// Каждая таблица, которая копит записи со временем, держит их ровно столько,
// сколько они на что-то отвечают, — и ни днём дольше: всё, что лежит в базе,
// выдаётся при её изъятии или утечке копии. Сроки и причины:
//
//   - отчёты о доступности нод (node_reports) — healthWindow, шесть часов.
//     Старше этого их не читает и сводка доступности; хранить дольше значило
//     бы держать запись «этот покупатель тогда-то пробовал эту ноду»;
//   - ключи идемпотентности — неделя (idempotencyTTL): платёжные системы
//     повторяют уведомления часами, неделя с запасом;
//   - журнал действий — EventsKeepDays, три месяца: спор об оплате случается
//     в пределах нескольких недель;
//   - посуточная история расхода — UsageKeepDays: в ней нет людей, только
//     день, нода и объём, и год с запасом нужен для графиков.
//
// Чего здесь нет. Накопительный расход (usage), «на связи» (presence) и
// время последней связи у покупателя не стареют сами: это не журнал, а одна
// строка на покупателя или пару «покупатель — нода», которая перезаписывается
// и уходит вместе с покупателем.

// UsageKeepDays — сколько дней держим посуточную историю расхода.
const UsageKeepDays = 400

// ForgetEvery — как часто панель убирает устаревшее, не дожидаясь
// перезапуска. Шесть часов — тот же срок, что у отчётов о доступности:
// дольше они не лежат.
const ForgetEvery = healthWindow

// Forget убирает всё, у чего вышел срок хранения.
//
// Одна неудача не останавливает остальные: недочищенный журнал не повод
// держать и отчёты.
func (s *Store) Forget(ctx context.Context) error {
	var errs []error
	if err := s.ForgetStaleReports(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := s.ForgetStaleIdempotency(ctx); err != nil {
		errs = append(errs, fmt.Errorf("ключи идемпотентности: %w", err))
	}
	if err := s.ForgetOldEvents(ctx, EventsKeepDays); err != nil {
		errs = append(errs, err)
	}
	if err := s.ForgetOldUsage(ctx, UsageKeepDays); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

// KeepForgetting зовёт Forget раз в every, пока не отменят контекст.
//
// Разовой уборки при запуске мало: панель работает месяцами, и обещанные
// сроки соблюдались бы только у тех, кто часто её перезапускает.
func (s *Store) KeepForgetting(ctx context.Context, every time.Duration, onError func(error)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Forget(ctx); err != nil && onError != nil && ctx.Err() == nil {
				onError(err)
			}
		}
	}
}

// ForgetStaleReports убирает отчёты о доступности старше окна, по которому
// считается сводка.
func (s *Store) ForgetStaleReports(ctx context.Context) error {
	edge := format(time.Now().UTC().Add(-healthWindow))
	if _, err := s.db.ExecContext(ctx, `DELETE FROM node_reports WHERE reported_at < ?`, edge); err != nil {
		return fmt.Errorf("очистка отчётов о доступности: %w", err)
	}
	return nil
}
