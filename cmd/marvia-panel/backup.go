package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/jytt8u/marvia/internal/envvar"
	"github.com/jytt8u/marvia/internal/panel"
)

func runVerifyBackup(out io.Writer, input io.Reader, path, output string) error {
	password := envvar.Get("MARVIA_BACKUP_PASSWORD")
	if password == "" {
		line, err := bufio.NewReader(io.LimitReader(input, 4097)).ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("чтение пароля из stdin: %w", err)
		}
		password = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
	}
	report, err := panel.VerifyPasswordBackup(context.Background(), path, password, output)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(out, "Копия проверена: схема и целостность базы в порядке\nКлиентов: %d\nНод: %d\n", report.Clients, report.Nodes)
	return err
}
