package foreign

import "testing"

func TestTheBuyerNameSharedByEveryNodeIsNotRepeatedInTheList(t *testing.T) {
	const uuid = "b831381d-6324-4d53-ad4f-8cda48b30811"
	link := func(host, name string) string {
		return "vless://" + uuid + "@" + host + ":443?security=none#" + name
	}
	sub := ParseList([]byte(link("a.example", "ОАЭ%20%C2%B7%20ae-1%20%C2%B7%20проверка2") + "\n" +
		link("b.example", "fi-1%20%C2%B7%20проверка2")))
	if sub.Links[0].Name != "ОАЭ · ae-1" || sub.Links[1].Name != "fi-1" {
		t.Fatalf("имена %q, %q", sub.Links[0].Name, sub.Links[1].Name)
	}

	// Разные хвосты — это не имя покупателя, а часть имени ноды.
	sub = ParseList([]byte(link("a.example", "Германия%20%C2%B7%20Франкфурт") + "\n" +
		link("b.example", "Нидерланды%20%C2%B7%20Амстердам")))
	if sub.Links[0].Name != "Германия · Франкфурт" {
		t.Fatalf("срезано лишнее: %q", sub.Links[0].Name)
	}

	// Одна нода — сравнивать не с чем, имя остаётся целым.
	sub = ParseList([]byte(link("a.example", "fi-1%20%C2%B7%20проверка2")))
	if sub.Links[0].Name != "fi-1 · проверка2" {
		t.Fatalf("у единственной ноды срезано имя: %q", sub.Links[0].Name)
	}
}
