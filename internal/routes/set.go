package routes

import (
	"net/netip"
	"slices"
	"strings"
)

// Set — набор подсетей, в котором быстро ищется адрес.
//
// Нужен там, где решение «мимо туннеля или в него» принимает не система по
// маршрутам, а сама программа на каждое соединение — так делает окно на
// Windows. Перебирать восемь с лишним тысяч подсетей на каждое соединение
// незачем: подсети сливаются в непересекающиеся отрезки и ищутся двоичным
// поиском — два десятка сравнений вместо тысяч.
type Set struct {
	spans []span
	count int
}

// span — отрезок адресов от lo до hi включительно, одного семейства.
type span struct{ lo, hi netip.Addr }

// NewSet собирает набор из строк вида 2.56.24.0/22. Неразборчивые строки
// пропускаются: одна битая строка в присланном списке не повод остаться без
// остальных. Сколько принято — Len.
func NewSet(prefixes []string) *Set {
	spans := make([]span, 0, len(prefixes))
	for _, raw := range prefixes {
		p, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		p = p.Masked()
		spans = append(spans, span{lo: p.Addr(), hi: lastAddr(p)})
	}
	count := len(spans)

	slices.SortFunc(spans, func(a, b span) int { return a.lo.Compare(b.lo) })

	// Сливаем пересекающиеся и вплотную идущие: после этого отрезки не
	// перекрываются, и двоичный поиск по началу находит единственного
	// кандидата. IPv4 и IPv6 при сортировке не перемешиваются — Compare
	// ставит все IPv4 раньше всех IPv6, — и сливаться друг с другом не могут.
	merged := spans[:0]
	for _, s := range spans {
		if n := len(merged); n > 0 {
			last := &merged[n-1]
			if last.lo.BitLen() == s.lo.BitLen() && (s.lo.Compare(last.hi) <= 0 || last.hi.Next() == s.lo) {
				if s.hi.Compare(last.hi) > 0 {
					last.hi = s.hi
				}
				continue
			}
		}
		merged = append(merged, s)
	}
	return &Set{spans: merged, count: count}
}

// Len — сколько подсетей принято при сборке, до слияния. Это число человек
// видит под переключателем: «в списке 8651 подсеть».
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return s.count
}

// Contains — входит ли адрес в набор. Пустой набор не содержит ничего.
func (s *Set) Contains(ip netip.Addr) bool {
	if s == nil || !ip.IsValid() {
		return false
	}
	ip = ip.Unmap()
	// Первый отрезок, который начинается правее адреса; кандидат — перед ним.
	i, _ := slices.BinarySearchFunc(s.spans, ip, func(sp span, target netip.Addr) int {
		if sp.lo.Compare(target) <= 0 {
			return -1
		}
		return 1
	})
	if i == 0 {
		return false
	}
	c := s.spans[i-1]
	return c.lo.BitLen() == ip.BitLen() && ip.Compare(c.hi) <= 0
}

// lastAddr — последний адрес подсети.
func lastAddr(p netip.Prefix) netip.Addr {
	a := p.Addr().As16()
	bits := p.Bits()
	if p.Addr().Is4() {
		bits += 96
	}
	for i := bits; i < 128; i++ {
		a[i/8] |= 1 << (7 - i%8)
	}
	out := netip.AddrFrom16(a)
	if p.Addr().Is4() {
		return out.Unmap()
	}
	return out
}
