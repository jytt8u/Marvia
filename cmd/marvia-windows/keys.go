package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/jytt8u/marvia/internal/client"
	"github.com/jytt8u/marvia/internal/foreign"
)

// Несколько ключей доступа — как подписки на телефоне.
//
// Доступ у человека бывает от двух продавцов сразу: свой ключ Marvia и чужая
// подписка, или два ключа от разных панелей. Раньше окно помнило один, и
// второй приходилось держать в блокноте и вставлять заново при каждой смене
// — вместе с риском вставить не тот.
//
// Ключей много, рабочий один — как на телефоне и в Hiddify, откуда взят
// образец. Рабочий по-прежнему лежит в файле account: его читают автозапуск
// и трей, и старые версии программы, если человек откатится, найдут ключ на
// прежнем месте. Список — отдельным файлом рядом.

// savedKey — один сохранённый ключ.
type savedKey struct {
	Name string `json:"name"`
	Link string `json:"link"`
}

// keyID — как окно ссылается на ключ: отпечаток, а не сама ссылка. В
// ссылке личный ключ покупателя, и гонять её туда-обратно ради кнопки
// «сделать рабочим» незачем.
func keyID(link string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(link)))
	return hex.EncodeToString(sum[:6])
}

func keysPath(dir string) string { return filepath.Join(dir, "keys.json") }

// loadKeys читает список. Списка ещё нет, а рабочий ключ есть — так
// выглядит обновление со старой версии: список начинается с него.
func loadKeys(dir, active string) []savedKey {
	var list []savedKey
	if raw, err := os.ReadFile(keysPath(dir)); err == nil && len(raw) < 1<<20 {
		_ = json.Unmarshal(raw, &list)
	}
	clean := list[:0]
	for _, k := range list {
		if k.Link = strings.TrimSpace(k.Link); k.Link != "" {
			clean = append(clean, k)
		}
	}
	if active != "" && findKey(clean, keyID(active)) < 0 {
		clean = append(clean, savedKey{Name: keyName(active), Link: active})
	}
	return clean
}

// saveKeys пишет список только для владельца и через временный файл: в
// нём личные ключи, а оборванная запись не должна потерять все разом.
func saveKeys(dir string, list []savedKey) error {
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := keysPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, keysPath(dir)); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func findKey(list []savedKey, id string) int {
	for i, k := range list {
		if keyID(k.Link) == id {
			return i
		}
	}
	return -1
}

// withKey добавляет ключ или переименовывает уже сохранённый. Повтор той же
// ссылки не двоится: человек вставляет ключ заново, когда сомневается, что
// он сохранился, и второй строки с тем же ключом не ждёт.
func withKey(list []savedKey, name, link string) []savedKey {
	link = strings.TrimSpace(link)
	name = strings.TrimSpace(name)
	if i := findKey(list, keyID(link)); i >= 0 {
		if name != "" {
			list[i].Name = name
		}
		return list
	}
	if name == "" {
		name = keyName(link)
	}
	return append(list, savedKey{Name: name, Link: link})
}

// withoutKey убирает ключ из списка.
func withoutKey(list []savedKey, id string) []savedKey {
	out := make([]savedKey, 0, len(list))
	for _, k := range list {
		if keyID(k.Link) != id {
			out = append(out, k)
		}
	}
	return out
}

// keyName — имя, когда человек его не ввёл: метка из ссылки или домен
// продавца. Он и отличает продавцов друг от друга, а «Ключ 1» и «Ключ 2» не
// отличают ничего. Правила те же, что у телефона (Store.nameFromLink).
func keyName(link string) string {
	link = strings.TrimSpace(foreign.FirstLine(link))
	if account, err := client.ParseAccountLink(link); err == nil {
		if account.Label != "" {
			return account.Label
		}
		if u, err := url.Parse(account.SubscriptionURL); err == nil {
			return u.Hostname()
		}
	}
	u, err := url.Parse(link)
	if err != nil {
		return "—"
	}
	if tag, err := url.PathUnescape(u.Fragment); err == nil && strings.TrimSpace(tag) != "" {
		return strings.TrimSpace(tag)
	}
	if host := u.Hostname(); host != "" {
		return host
	}
	return "—"
}

// KeyView — ключ так, как его видит окно: без самой ссылки.
type KeyView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
	// Foreign — чужая подписка или ссылка: окно подписывает её протоколом,
	// а не «ключ Marvia».
	Foreign bool `json:"foreign"`
}

func keyViews(list []savedKey, active string) []KeyView {
	out := make([]KeyView, 0, len(list))
	activeID := ""
	if active != "" {
		activeID = keyID(active)
	}
	for _, k := range list {
		id := keyID(k.Link)
		out = append(out, KeyView{ID: id, Name: k.Name, Active: id == activeID, Foreign: foreign.IsForeign(k.Link)})
	}
	return out
}
