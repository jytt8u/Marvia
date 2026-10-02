package importer

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// notesShown — сколько покупателей с заметками печатать поимённо. Больше
// в терминал не помещается, а суть видна и по первым: заметки повторяются.
const notesShown = 30

// Movable — сколько покупателей переедут: у них есть хоть один набор, который
// понимает нода Marvia.
func (p *Plan) Movable() int {
	n := 0
	for _, c := range p.Customers {
		if len(c.User.VLESS) > 0 || len(c.User.Trojan) > 0 {
			n++
		}
	}
	return n
}

// Report печатает, что и как переедет. Ничего не пишет и ничего не решает:
// решает продавец, прочитав.
func (p *Plan) Report(w io.Writer, from string) {
	pf := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }

	pf("Переезд с %s · %s\n\n", p.Source, from)

	movable := p.Movable()
	withNotes := 0
	for _, c := range p.Customers {
		if len(c.Notes) > 0 {
			withNotes++
		}
	}
	pf("Покупатели: %d\n", len(p.Customers))
	pf("  переедут               %d\n", movable)
	if withNotes > 0 {
		pf("  из них с заметками     %d — список ниже\n", withNotes)
	}
	if rest := len(p.Customers) - movable; rest > 0 {
		pf("  нечего переносить      %d — только VMess или Shadowsocks и нет подписки\n", rest)
	}

	pf("\nВходы прежней панели\n")
	for _, in := range p.Inbounds {
		verdict, why := in.Verdict()
		mark := map[Verdict]string{OneCommand: "✓", Manual: "~", Unsupported: "✕"}[verdict]
		pf("  %s %s — %s\n", mark, in.Title(), customersWord(in.Customers))
		switch verdict {
		case OneCommand:
			pf("      прежние ссылки подойдут, если нода Marvia встанет на этот же сервер\n")
			pf("      с теми же ключами. В строку установки ноды из панели перед «sh» добавь:\n")
			pf("        %s\n", in.NodeEnv())
		case Manual:
			pf("      %s — см. docs/guide.md, «Переезд с Marzban и 3x-ui»\n", why)
		case Unsupported:
			pf("      %s; кому есть чем, новые ссылки придут с подпиской\n", why)
		}
		if in.Vision > 0 && verdict != Unsupported {
			pf("      у %s ссылки с Vision: такие нода не примет, новые придут с подпиской\n", customersOf(in.Vision))
		}
	}

	pf("\nПодписка\n")
	where := "домен прежней панели"
	if p.SubDomain != "" {
		where = p.SubDomain
	}
	pf("  Прежние адреса …/%s/<токен> продолжат работать, когда %s\n", p.SubPath, where)
	pf("  будет вести на эту панель. Приложение покупателя само обновит по ним\n")
	pf("  подписку и получит ссылки на ноды Marvia — даже там, где прежние не подошли.\n")
	if p.SubPort > 0 {
		pf("  Подписка 3x-ui жила на порту %d: его тоже надо направить на панель —\n", p.SubPort)
		pf("  обратным прокси или перенаправлением порта (см. руководство).\n")
	}

	if len(p.Warnings) > 0 {
		pf("\nПредупреждения\n")
		for _, warn := range p.Warnings {
			pf("  • %s\n", warn)
		}
	}

	if withNotes > 0 {
		pf("\nПокупатели с заметками\n")
		shown := 0
		for _, c := range p.Customers {
			if len(c.Notes) == 0 {
				continue
			}
			if shown == notesShown {
				pf("  …и ещё %d\n", withNotes-shown)
				break
			}
			pf("  %s — %s\n", c.Name, strings.Join(c.Notes, "; "))
			shown++
		}
	}
}

// customersWord — «1 покупатель», «3 покупателя», «12 покупателей».
func customersWord(n int) string {
	word := "покупателей"
	switch {
	case n%100 >= 11 && n%100 <= 14:
	case n%10 == 1:
		word = "покупатель"
	case n%10 >= 2 && n%10 <= 4:
		word = "покупателя"
	}
	return strconv.Itoa(n) + " " + word
}

// customersOf — «у 1 покупателя», «у 5 покупателей»: после «у» число
// требует родительного падежа.
func customersOf(n int) string {
	if n%10 == 1 && n%100 != 11 {
		return strconv.Itoa(n) + " покупателя"
	}
	return strconv.Itoa(n) + " покупателей"
}
