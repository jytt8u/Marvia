// Package version задаёт один порядок выпусков для панели и приложений:
// иначе нода получает alpha.6, а покупатель на alpha.5 не видит обновления.
package version

import (
	"strconv"
	"strings"
)

// Older сообщает, идёт ли a раньше b: alpha.5 < alpha.6 < beta.1 < rc.1
// < выпуск без хвоста. Непонятные версии не предлагаем заменять автоматически.
func Older(a, b string) bool {
	pa, preA, okA := parts(a)
	pb, preB, okB := parts(b)
	if !okA || !okB {
		return false
	}
	for i := range pa {
		if pa[i] != pb[i] {
			return pa[i] < pb[i]
		}
	}
	switch {
	case preA == preB, preA == "":
		return false
	case preB == "":
		return true
	}
	return olderPrerelease(strings.Split(preA, "."), strings.Split(preB, "."))
}

// Newer использует обратный порядок Older, чтобы клиент и панель не
// расходились в правилах для хвоста, префикса v и метаданных сборки.
func Newer(a, b string) bool { return Older(b, a) }

// Числа сравниваем по значению: alpha.9 раньше alpha.10. Слова — по
// алфавиту, число раньше слова; общий короткий хвост раньше длинного.
func olderPrerelease(a, b []string) bool {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		na, errA := strconv.Atoi(a[i])
		nb, errB := strconv.Atoi(b[i])
		switch {
		case errA == nil && errB == nil:
			return na < nb
		case errA == nil:
			return true
		case errB == nil:
			return false
		}
		return a[i] < b[i]
	}
	return len(a) < len(b)
}

func parts(v string) ([3]int, string, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+") // Метаданные сборки не меняют порядок выпусков.
	core, pre, hasPre := strings.Cut(v, "-")
	fields := strings.Split(core, ".")
	// Старый клиент понимал 0.13 как 0.13.0; сохраняем совместимость.
	if len(fields) > 3 || (hasPre && pre == "") {
		return out, "", false
	}
	for i, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil || n < 0 {
			return out, "", false
		}
		out[i] = n
	}
	return out, pre, true
}
