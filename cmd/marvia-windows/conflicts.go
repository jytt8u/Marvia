//go:build windows

package main

import "strings"

// Программы, которые мешают туннелю, если работают рядом с ним.
//
// Люди приходят к Marvia с уже стоящими средствами обхода. zapret и
// GoodbyeDPI помехой больше не считаются: их поддельные пакеты мост
// выбрасывает сам (internal/tunbridge/decoy.go), и туннель с ними уживается —
// а без VPN они человеку нужны. Второй
// VPN-клиент спорит за маршруты, а Hiddify на том же драйвере Wintun удаляет
// чужой адаптер и оставляет системный прокси. Человек этого не видит; окно
// должно назвать программу и сказать, что сделать.

type conflictRule struct {
	// key — ключ сообщения в словаре; names — имена процессов без .exe, в
	// нижнем регистре.
	key   string
	names []string
}

var conflictRules = []conflictRule{
	{key: "conflictWarp", names: []string{"warp-svc", "cloudflare warp"}},
	{key: "conflictHiddify", names: []string{"hiddify", "hiddifycli"}},
	{key: "conflictVPN", names: []string{"v2rayn", "nekoray", "nekobox", "clash-verge", "clash verge", "amneziavpn", "outline", "happ", "v2raytun"}},
}

// conflictsAmong — какие правила сработали на этом списке процессов. Порядок
// — как в conflictRules.
func conflictsAmong(processes []string) []string {
	running := make(map[string]bool, len(processes))
	for _, p := range processes {
		running[strings.ToLower(strings.TrimSuffix(p, ".exe"))] = true
	}
	var found []string
	for _, rule := range conflictRules {
		for _, name := range rule.names {
			if running[name] {
				found = append(found, rule.key)
				break
			}
		}
	}
	return found
}

// conflictTexts — сообщения на языке окна, без повторов.
func conflictTexts(keys []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range keys {
		if !seen[k] {
			seen[k] = true
			out = append(out, say(k))
		}
	}
	return out
}
