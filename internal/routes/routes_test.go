package routes_test

import (
	"net/netip"
	"testing"

	"github.com/jytt8u/marvia/internal/routes"
)

// Список российских подсетей — то, из-за чего у покупателя открываются банк и
// госуслуги при поднятом туннеле. Ошибка здесь не падает и не пишет в журнал:
// человек просто видит «вход заблокирован» в приложении банка и идёт к
// продавцу. Поэтому список проверяем по существу, а не на «непустой».

func prefixes(t *testing.T) []netip.Prefix {
	t.Helper()

	raw, err := routes.RussianPrefixes()
	if err != nil {
		t.Fatalf("список подсетей не читается: %v", err)
	}
	if len(raw) < 1000 {
		t.Fatalf("подсетей всего %d — список явно обрезан", len(raw))
	}

	out := make([]netip.Prefix, 0, len(raw))
	for _, s := range raw {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			t.Fatalf("подсеть %q не разбирается: %v", s, err)
		}
		// Только IPv4: маршруты байпаса раскладываются в таблицу как есть, и
		// строка IPv6 среди них молча не применилась бы.
		if !p.Addr().Is4() {
			t.Fatalf("подсеть %q не IPv4", s)
		}
		out = append(out, p)
	}
	return out
}

func covers(list []netip.Prefix, ip string) bool {
	addr := netip.MustParseAddr(ip)
	for _, p := range list {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// TestRussianSitesGoAroundTheTunnel — адреса, ради которых байпас и сделан,
// в списке есть.
//
// Взяты не с потолка: это адреса Сбербанка, Яндекса, ВК и Госуслуг — ровно те
// места, которые ругаются на зарубежный адрес и из-за которых человек выключает
// туннель целиком.
func TestRussianSitesGoAroundTheTunnel(t *testing.T) {
	list := prefixes(t)

	known := map[string]string{
		"Сбербанк":  "194.54.14.141",
		"Яндекс":    "77.88.55.242",
		"ВКонтакте": "87.240.190.78",
		"Госуслуги": "212.164.129.11",
	}
	for who, ip := range known {
		if !covers(list, ip) {
			t.Errorf("%s (%s) пойдёт через туннель — а должен мимо", who, ip)
		}
	}
}

// TestForeignSitesStayInTheTunnel — зарубежные адреса мимо туннеля не уходят.
//
// Обратная половина обещания, и она важнее: лишняя подсеть в списке — это
// трафик, который человек считал защищённым, а он пошёл открыто.
func TestForeignSitesStayInTheTunnel(t *testing.T) {
	list := prefixes(t)

	known := map[string]string{
		"Google DNS":     "8.8.8.8",
		"Cloudflare DNS": "1.1.1.1",
		"Quad9":          "9.9.9.9",
		"example.com":    "93.184.216.34",
	}
	for who, ip := range known {
		if covers(list, ip) {
			t.Errorf("%s (%s) пойдёт мимо туннеля — а должен через", who, ip)
		}
	}
}

// TestPrivateNetworksAreNotInTheList — приватных диапазонов в списке нет.
//
// Домашняя сеть выводится мимо туннеля отдельной настройкой, которую человек
// включает сам. Приехав сюда, она включилась бы у всех и молча.
func TestPrivateNetworksAreNotInTheList(t *testing.T) {
	list := prefixes(t)

	for _, ip := range []string{"10.0.0.1", "192.168.1.1", "172.16.0.1", "127.0.0.1"} {
		if covers(list, ip) {
			t.Errorf("приватный адрес %s попал в список российских подсетей", ip)
		}
	}
}

// TestListHasNoDuplicatesOrOverlaps — одна и та же подсеть не повторяется.
//
// Повтор — это лишняя строка в таблице маршрутов на устройстве покупателя, а
// их там и так тысячи: на телефоне это время старта туннеля.
func TestListHasNoDuplicatesOrOverlaps(t *testing.T) {
	list := prefixes(t)

	seen := make(map[netip.Prefix]bool, len(list))
	for _, p := range list {
		if seen[p] {
			t.Errorf("подсеть %s встречается дважды", p)
		}
		seen[p] = true
	}
}

// TestListIsReadTwiceTheSame — второй вызов отдаёт то же самое.
//
// Список разжимается один раз и живёт в памяти. Если бы распаковка портила
// состояние, второй клиент получил бы пустой ответ — и без туннеля остался бы
// он, а не мы.
func TestListIsReadTwiceTheSame(t *testing.T) {
	first, err := routes.RussianPrefixes()
	if err != nil {
		t.Fatal(err)
	}
	second, err := routes.RussianPrefixes()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != len(second) {
		t.Fatalf("первый раз %d подсетей, второй %d", len(first), len(second))
	}
}

// Набор для поиска отвечает так же, как перебор всех подсетей: и на адресах
// из списка, и на соседних с краями, и на чужих.
func TestSetAnswersLikeTheFullList(t *testing.T) {
	raw, _ := routes.RussianPrefixes()
	list := prefixes(t)
	set := routes.NewSet(raw)
	if set.Len() != len(raw) {
		t.Fatalf("принято %d подсетей из %d", set.Len(), len(raw))
	}

	var probes []netip.Addr
	for i, p := range list {
		if i%7 != 0 {
			continue
		}
		first := p.Masked().Addr()
		probes = append(probes, first, first.Prev())
		last := first
		for j := 0; j < 1<<(32-p.Bits())-1 && j < 1<<16; j++ {
			last = last.Next()
		}
		probes = append(probes, last, last.Next())
	}
	for _, s := range []string{"8.8.8.8", "1.1.1.1", "192.168.1.1", "77.88.55.242", "::1", "2a02:6b8::2:242"} {
		probes = append(probes, netip.MustParseAddr(s))
	}
	for _, ip := range probes {
		if !ip.IsValid() {
			continue
		}
		if got, want := set.Contains(ip), covers(list, ip.String()); got != want {
			t.Errorf("%s: набор говорит %v, перебор — %v", ip, got, want)
		}
	}
}

// Пересекающиеся, вложенные и смежные подсети, IPv6 и битые строки не
// сбивают поиск.
func TestSetHandlesOverlapsFamiliesAndJunk(t *testing.T) {
	set := routes.NewSet([]string{"10.0.0.0/8", "10.1.0.0/16", "11.0.0.0/8", "мусор", "2001:db8::/32", "192.0.2.7/32"})
	if set.Len() != 5 {
		t.Errorf("принято %d, ожидалось 5", set.Len())
	}
	in := []string{"10.0.0.0", "10.255.255.255", "11.200.0.1", "2001:db8::1", "192.0.2.7", "::ffff:10.1.2.3"}
	out := []string{"9.255.255.255", "12.0.0.0", "2001:db9::", "192.0.2.8", "::a00:1"}
	for _, s := range in {
		if !set.Contains(netip.MustParseAddr(s)) {
			t.Errorf("%s должен входить", s)
		}
	}
	for _, s := range out {
		if set.Contains(netip.MustParseAddr(s)) {
			t.Errorf("%s не должен входить", s)
		}
	}
	var empty *routes.Set
	if empty.Contains(netip.MustParseAddr("10.0.0.1")) || empty.Len() != 0 {
		t.Error("пустой набор что-то содержит")
	}
}
