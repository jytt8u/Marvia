//go:build windows

package main

import (
	"strings"

	"golang.org/x/sys/windows"

	"github.com/jytt8u/marvia/internal/redact"
)

// Отчёт об ошибке для продавца или разработчика.
//
// «Не работает» без подробностей не чинится, а пересказывать журнал по
// скриншотам человек не станет. Кнопка «Скопировать отчёт» кладёт в буфер то,
// что нужно для разбора: версию, Windows, состояние, настройки, помехи и хвост
// журнала, — так же, как это делает журнал на телефоне.
//
// Отчёт уходит людям, которых человек выбрал сам, иногда в публичный issue.
// Поэтому в нём нет ни ссылки доступа, ни адресов: строки журнала проходят
// redact.Text, который закрывает и ключи в ссылках, и IP-адреса. Имя ноды
// остаётся — без него продавец не поймёт, о какой речь, а адреса он знает и
// так.

// reportTail — сколько последних строк журнала идёт в отчёт. Больше — и
// отчёт не влезет в сообщение мессенджера; меньше — и начало сбоя уйдёт за край.
const reportTail = 80

// Report собирает отчёт на языке окна.
func (c *Controller) Report() string {
	st := c.Status()
	set := c.Settings()
	var b strings.Builder
	line := func(key string, args ...any) { b.WriteString(sayf(key, args...) + "\n") }

	line("reportHeader", version)
	v := windows.RtlGetVersion()
	line("reportWindows", v.MajorVersion, v.MinorVersion, v.BuildNumber, yesNo(st.Elevated))
	line("reportState", st.State, st.Node, st.Transport, st.PingMS)
	if st.Reason != "" {
		line("reportReason", redact.Text(st.Reason))
	}
	if st.Check != "" {
		line("reportCheck", st.Check)
	}
	// Ноль в настройках значит «как в программе»: в отчёт — действующий размер.
	mtu := set.MTU
	if mtu == 0 {
		mtu = set.MTUDefault
	}
	line("reportSettings",
		yesNo(set.BypassRussian), yesNo(set.LANOutside), yesNo(set.KillSwitch),
		set.DNS, yesNo(set.DNSSecure), yesNo(set.IPv6), yesNo(set.Fragment), yesNo(set.TCPOnly), fingerprintName(set.Fingerprint), mtu, yesNo(set.AutoFix))
	// Сколько, а не какие: отчёт уходит продавцу, а список сайтов — это
	// список того, куда человек ходит. Для разбора «почему сайт видит не ту
	// страну» хватает знать, что правила есть.
	if len(set.SitesDirect)+len(set.SitesTunnel) > 0 {
		line("reportSites", len(set.SitesDirect), len(set.SitesTunnel))
	}
	for _, conflict := range st.Conflicts {
		line("reportConflict", conflict)
	}
	if st.Proxy != "" {
		owner := st.ProxyOwner
		if owner == "" {
			owner = "?"
		}
		line("reportProxy", redact.Text(st.ProxyServer), owner)
	}

	b.WriteString("\n")
	journal := c.log.snapshot()
	if len(journal) > reportTail {
		journal = journal[len(journal)-reportTail:]
	}
	for _, l := range journal {
		b.WriteString(redact.Text(l) + "\n")
	}
	return b.String()
}

func yesNo(v bool) string {
	if v {
		return say("reportYes")
	}
	return say("reportNo")
}

// fingerprintName — отпечаток для отчёта: выбранный человеком или «как у
// продавца». Продавцу важно отличить второе от первого: свой отпечаток он
// видит в панели, а заменённый — только здесь.
func fingerprintName(chosen string) string {
	if chosen == "" {
		return say("reportFPSeller")
	}
	return chosen
}
