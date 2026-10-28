package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jytt8u/marvia/internal/importer"
	"github.com/jytt8u/marvia/internal/panel"
)

// importOptions — переезд с чужой панели, см. internal/importer.
type importOptions struct {
	source  string // marzban или 3x-ui
	from    string // база прежней панели
	xray    string // xray_config.json Marzban
	subPath string // путь подписки прежней панели, если не /sub/
	apply   bool   // записать, а не только показать
}

// runImport показывает, что переедет, и с -import-apply переносит.
//
// По умолчанию — только отчёт. Переезд меняет то, с чем покупатели
// подключаются завтра утром, и продавец должен увидеть это до записи, а не
// после: что переедет, у кого ссылки сменятся, какую строку дать установщику
// ноды.
func runImport(out io.Writer, opts importOptions, panelDB string) error {
	if opts.from == "" {
		return errors.New("укажи базу прежней панели: -import-db /var/lib/marzban/db.sqlite3 или -import-db /etc/x-ui/x-ui.db")
	}

	var (
		plan *importer.Plan
		err  error
	)
	switch strings.ToLower(opts.source) {
	case "marzban":
		plan, err = importer.ReadMarzban(opts.from, opts.xray, time.Now())
	case "3x-ui", "x-ui", "3xui":
		plan, err = importer.ReadXUI(opts.from, time.Now())
	default:
		return fmt.Errorf("-import %q: умею marzban и 3x-ui", opts.source)
	}
	if err != nil {
		return err
	}
	if opts.subPath != "" {
		plan.SubPath = strings.Trim(opts.subPath, "/")
	}

	plan.Report(out, opts.from)
	if !opts.apply {
		fmt.Fprintln(out, "\nЭто проверка: в панель ничего не записано. Записать — та же команда с -import-apply.")
		return nil
	}

	store, err := panel.Open(panelDB)
	if err != nil {
		return err
	}
	// Отложенные вызовы идут в обратном порядке: хозяин возвращается уже
	// после закрытия базы, когда SQLite больше ничего не создаст.
	defer keepOwner(panelDB)
	defer store.Close()
	ctx := context.Background()

	var (
		created, existed, empty int
		skipped                 []string
	)
	for _, c := range plan.Customers {
		res, err := store.ImportUser(ctx, c.User)
		switch {
		case errors.Is(err, panel.ErrNoAccess):
			empty++
			continue
		case err != nil:
			if strings.Contains(err.Error(), "database is locked") || strings.Contains(err.Error(), "SQLITE_BUSY") {
				return fmt.Errorf("%s: база панели занята — останови панель на время переноса (systemctl stop marvia-panel) и повтори: перенесённые второй раз не заведутся", c.Name)
			}
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		if res.Existed {
			existed++
		} else {
			created++
		}
		for _, s := range res.Skipped {
			skipped = append(skipped, c.Name+" — "+s)
		}
	}
	if plan.SubPath != "sub" {
		if err := store.AddLegacySubPath(ctx, plan.SubPath); err != nil {
			return err
		}
	}

	fmt.Fprintf(out, "\nЗаписано в %s\n", panelDB)
	fmt.Fprintf(out, "  перенесено             %d\n", created)
	if existed > 0 {
		fmt.Fprintf(out, "  уже были перенесены    %d — не тронуты\n", existed)
	}
	if empty > 0 {
		fmt.Fprintf(out, "  нечего переносить      %d\n", empty)
	}
	if len(skipped) > 0 {
		fmt.Fprintln(out, "\nНе перенеслось")
		for _, s := range skipped {
			fmt.Fprintf(out, "  %s\n", s)
		}
	}
	return nil
}
