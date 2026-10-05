package panel

import (
	"context"
	"time"
)

// SetReleaseURL подменяет адрес последнего релиза на время проверки.
func SetReleaseURL(u string) (restore func()) {
	was := releaseURL
	releaseURL = u
	return func() { releaseURL = was }
}

// BackdateReports сдвигает все отчёты о доступности в прошлое: тестам на срок
// хранения нужно изобразить отчёт, которому уже не шесть часов.
func (s *Store) BackdateReports(ctx context.Context, by time.Duration) error {
	_, err := s.db.ExecContext(ctx, `UPDATE node_reports SET reported_at = ?`, format(time.Now().UTC().Add(-by)))
	return err
}

// Rows — сколько строк в таблице. Обещания о хранении проверяются по самой
// базе, а не по коду, который в неё пишет.
func (s *Store) Rows(ctx context.Context, table string) int {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n)
	return n
}

// Columns перечисляет столбцы таблицы.
func (s *Store) Columns(ctx context.Context, table string) []string {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			out = append(out, name)
		}
	}
	return out
}

// Tables перечисляет все таблицы базы.
func (s *Store) Tables(ctx context.Context) []string {
	rows, err := s.db.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			out = append(out, name)
		}
	}
	return out
}

// Exec выполняет запрос к базе напрямую: тестам обновления нужно положить
// данные в том виде, в каком их оставила прежняя версия.
func (s *Store) Exec(ctx context.Context, query string, args ...any) error {
	_, err := s.db.ExecContext(ctx, query, args...)
	return err
}
