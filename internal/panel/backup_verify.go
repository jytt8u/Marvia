package panel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

type BackupReport struct {
	Clients int
	Nodes   int
}

// VerifyPasswordBackup проверяет именно присланную схему. Open использовать
// для копии нельзя: миграции создали бы отсутствующие таблицы, и повреждённая
// копия выглядела бы пригодной. Вывод расшифровки возможен только после проверки.
func VerifyPasswordBackup(ctx context.Context, path, password, output string) (BackupReport, error) {
	var report BackupReport
	f, err := os.Open(path)
	if err != nil {
		return report, fmt.Errorf("чтение копии: %w", err)
	}
	sealed, err := io.ReadAll(io.LimitReader(f, maxSealedBackup+1024))
	_ = f.Close()
	if err != nil {
		return report, fmt.Errorf("чтение копии: %w", err)
	}
	if len(sealed) > maxSealedBackup+512 {
		return report, errors.New("копия больше предела проверки")
	}
	plain, err := OpenPasswordBackup(sealed, password)
	if err != nil {
		return report, err
	}
	defer clear(plain)
	dir, err := os.MkdirTemp("", "marvia-verify-*")
	if err != nil {
		return report, fmt.Errorf("каталог проверки: %w", err)
	}
	defer os.RemoveAll(dir)
	dbPath := filepath.Join(dir, "panel.db")
	if err := os.WriteFile(dbPath, plain, 0o600); err != nil {
		return report, fmt.Errorf("временная база: %w", err)
	}
	uriPath := filepath.ToSlash(dbPath)
	if !strings.HasPrefix(uriPath, "/") {
		uriPath = "/" + uriPath
	}
	uri := &url.URL{Scheme: "file", Path: uriPath, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return report, fmt.Errorf("открытие копии: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return report, errors.New("целостность базы в копии нарушена")
	}
	// Эталон создаётся отдельно, а не поверх копии: схема здесь всегда та,
	// которую ожидает запущенная версия панели.
	expected, err := Open(":memory:")
	if err != nil {
		return report, err
	}
	defer expected.Close()
	if err := compareBackupSchema(ctx, expected.db, db); err != nil {
		return report, err
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return report, errors.New("не удалось проверить связи базы")
	}
	broken := rows.Next()
	readErr := rows.Err()
	_ = rows.Close()
	if broken || readErr != nil {
		return report, errors.New("связи записей базы в копии нарушены")
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM users").Scan(&report.Clients); err != nil {
		return report, fmt.Errorf("подсчёт клиентов: %w", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM nodes").Scan(&report.Nodes); err != nil {
		return report, fmt.Errorf("подсчёт нод: %w", err)
	}
	if output != "" {
		out, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return report, fmt.Errorf("файл восстановления (он не должен существовать): %w", err)
		}
		_, writeErr := out.Write(plain)
		syncErr := out.Sync()
		closeErr := out.Close()
		if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
			_ = os.Remove(output)
			return report, fmt.Errorf("запись восстановленной базы: %w", err)
		}
	}
	return report, nil
}

type backupColumn struct {
	Type       string
	PrimaryKey int
}

func compareBackupSchema(ctx context.Context, expected, actual *sql.DB) error {
	rows, err := expected.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, table := range tables {
		want, err := backupColumns(ctx, expected, table)
		if err != nil {
			return err
		}
		have, err := backupColumns(ctx, actual, table)
		if err != nil {
			return err
		}
		for name, column := range want {
			if other, ok := have[name]; !ok || column != other {
				return fmt.Errorf("схема копии не подходит этой версии панели: таблица %s, поле %s", table, name)
			}
		}
	}
	return nil
}

func backupColumns(ctx context.Context, db *sql.DB, table string) (map[string]backupColumn, error) {
	rows, err := db.QueryContext(ctx, `PRAGMA table_info("`+strings.ReplaceAll(table, `"`, `""`)+`")`)
	if err != nil {
		return nil, fmt.Errorf("чтение схемы копии: %w", err)
	}
	defer rows.Close()
	columns := make(map[string]backupColumn)
	for rows.Next() {
		var cid, notNull, pk int
		var name, kind string
		var def sql.NullString
		if err := rows.Scan(&cid, &name, &kind, &notNull, &def, &pk); err != nil {
			return nil, err
		}
		columns[name] = backupColumn{Type: strings.ToUpper(kind), PrimaryKey: pk}
	}
	return columns, rows.Err()
}
