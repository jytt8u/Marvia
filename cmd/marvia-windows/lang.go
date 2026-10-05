//go:build windows

package main

import (
	"fmt"
	"sync/atomic"
)

// Сообщения программы на языке окна.
//
// Часть того, что человек читает, приходит не из разметки, а отсюда: «нужны
// права администратора», «нода не отвечает», строки журнала. Английская кнопка
// с русской ошибкой под ней выглядит хуже, чем честно русское окно, поэтому
// язык знает и программа.
//
// При первом запуске окно предлагает выбрать язык; до выбора показывает язык
// системы. После этого выбор хранится в каталоге настроек приложения.
//
// Хранится в atomic, а не под замком: пишет его одна ручка, читают — сторож,
// журнал и обработчики, и заводить ради одного значения ещё один замок рядом с
// замком контроллера значит завести и порядок их взятия.
var uiLang atomic.Value

func init() { uiLang.Store("ru") }

// setUILang запоминает язык окна. Незнакомый оставляет прежний: соврать
// молчанием лучше, чем показать пустые строки.
func setUILang(lang string) {
	if _, known := messages[lang]; !known {
		return
	}
	uiLang.Store(lang)
}

// say отдаёт сообщение на языке окна.
//
// Ключ, а не строка: так недостающий перевод виден в словаре, а не всплывает
// у человека на экране.
func say(key string) string {
	lang, _ := uiLang.Load().(string)
	if m, ok := messages[lang][key]; ok {
		return m
	}
	// Русский — запасной: он заполнен всегда, потому что на нём пишут.
	if m, ok := messages["ru"][key]; ok {
		return m
	}
	return key
}

var messages = map[string]map[string]string{
	"ru": {
		"webViewTitle":    "Для окна Marvia нужен WebView2",
		"webViewMissing":  "Не найден Microsoft Edge WebView2 Runtime. VPN не запущен.\n\nУстановите Evergreen Runtime с официальной страницы Microsoft и снова откройте Marvia. Ключи и настройки сохранены.\n\nОткрыть официальную страницу загрузки?",
		"webViewFailed":   "Не удалось открыть окно Marvia. Проверьте или восстановите Microsoft Edge WebView2 Runtime и запустите приложение снова. VPN остановлен; ключи и настройки сохранены.",
		"noAccount":       "не задана ссылка доступа",
		"replaceKeyTitle": "Заменить ключ доступа?",
		"replaceKeyBody":  "Ссылка пришла извне — из браузера или другой программы. После замены трафик пойдёт через серверы того, кто её выдал. Соглашайся, только если ссылку прислал твой продавец.",
		"logKeyKept":      "ссылка извне отклонена, ключ оставлен прежний",
		"needAdmin":       "нужны права администратора: без них Windows не даст создать сетевой адаптер",
		"proxyInEnv":      "прокси остался в переменных окружения — его убирает та программа, которая поставила",
		"nodeSilent":      "нода не отвечает — туннель поднят, но трафик через неё не идёт",
		"badRequest":      "не разобрал запрос",
		"accountLink":     "ссылка доступа",
		"privateKey":      "личный ключ",
		"bridge":          "сетевой мост",

		"logKeySaved":       "ключ доступа сохранён",
		"logProxyGone":      "системный прокси снят",
		"logTunnelUp":       "туннель поднят через %s",
		"logTunnelDown":     "туннель убран, маршруты сняты",
		"logNoConnect":      "не подключилось: %v",
		"logCleanup":        "при уборке: %v",
		"logConn":           "соединение: %v",
		"logNodeBack":       "нода снова отвечает",
		"logChosen":         "выбрана нода %s",
		"notConnected":      "туннель не поднят",
		"logMoved":          "переехали на %s",
		"logNodeSilent":     "нода не отвечает на %d проверки подряд: %v",
		"logAdapter":        "создаю сетевой адаптер и настраиваю маршруты",
		"logNodePicked":     "выбрана нода %s, задержка %d мс",
		"autostartFailed":   "планировщик отказал: задачу может поставить только администратор",
		"logAutostartOn":    "запуск вместе с Windows включён",
		"logAutostartOff":   "запуск вместе с Windows выключен",
		"bypassForeign":     "этот список даёт только панель Marvia, а подписка чужая",
		"logBypassCount":    "российских подсетей мимо туннеля: %d",
		"logBypassFailed":   "российский список не скачался: %v",
		"logBypassOff":      "российские сайты снова идут через туннель",
		"sellerNoLink":      "продавец не оставил такой ссылки",
		"trayRemindTitle":   "Marvia: пора продлить",
		"trayRemindExpiry":  "До конца доступа дней: %d. Продлите заранее, чтобы VPN не встал. Нажмите, чтобы открыть «Продлить».",
		"trayRemindTraffic": "Осталось %d%% трафика. Продлите заранее, чтобы VPN не встал. Нажмите, чтобы открыть «Продлить».",
		"sellerBadLink":     "ссылка продавца не прошла проверку и не открыта",
		"logSellerBadLink":  "ссылка продавца отвергнута: %v",
		"dnsBad":            "нужен адрес IPv4 вида 1.1.1.1",
		"dnsLocal":          "это адрес локальной сети: из туннеля он не виден",
		"logNamesSecure":    "имена идут к %s по HTTPS: нода видит только соединение с резолвером",
		"logNamesPlain":     "имена идут открытым DNS через туннель: нода их видит",
		"keyGone":           "такого ключа уже нет в списке",
		"logKeyUsed":        "рабочий ключ — %s",
		"logKeyRemoved":     "ключ %s убран",
	},
	"en": {
		"webViewTitle":    "Marvia needs WebView2 to display its window",
		"webViewMissing":  "Microsoft Edge WebView2 Runtime was not found. The VPN has not started.\n\nInstall the Evergreen Runtime from the official Microsoft page and open Marvia again. Your keys and settings are kept.\n\nOpen the official download page?",
		"webViewFailed":   "Marvia could not open its window. Check or repair Microsoft Edge WebView2 Runtime and start the app again. The VPN has stopped; your keys and settings are kept.",
		"noAccount":       "no access key set",
		"replaceKeyTitle": "Replace the access key?",
		"replaceKeyBody":  "This link came from outside — a browser or another program. After replacing it, your traffic goes through the servers of whoever issued it. Agree only if the link came from your seller.",
		"logKeyKept":      "outside link declined, the old key is kept",
		"needAdmin":       "administrator rights are required: without them Windows will not create a network adapter",
		"proxyInEnv":      "the proxy is still in environment variables — only the program that set it can remove it",
		"nodeSilent":      "the node is not responding — the tunnel is up, but no traffic goes through it",
		"badRequest":      "could not parse the request",
		"accountLink":     "access key",
		"privateKey":      "private key",
		"bridge":          "network bridge",

		"logKeySaved":       "access key saved",
		"logProxyGone":      "system proxy removed",
		"logTunnelUp":       "tunnel up through %s",
		"logTunnelDown":     "tunnel removed, routes cleared",
		"logNoConnect":      "could not connect: %v",
		"logCleanup":        "while cleaning up: %v",
		"logConn":           "connection: %v",
		"logNodeBack":       "the node responds again",
		"logChosen":         "node %s chosen",
		"notConnected":      "the tunnel is not up",
		"logMoved":          "moved to %s",
		"logNodeSilent":     "the node failed %d checks in a row: %v",
		"logAdapter":        "creating the network adapter and setting up routes",
		"logNodePicked":     "picked node %s, latency %d ms",
		"autostartFailed":   "the task scheduler refused: only an administrator can create the task",
		"logAutostartOn":    "start with Windows enabled",
		"logAutostartOff":   "start with Windows disabled",
		"bypassForeign":     "only a Marvia panel provides this list, and this subscription is not one",
		"logBypassCount":    "Russian subnets around the tunnel: %d",
		"logBypassFailed":   "the Russian list did not download: %v",
		"logBypassOff":      "Russian sites go through the tunnel again",
		"sellerNoLink":      "your seller left no such link",
		"trayRemindTitle":   "Marvia: time to renew",
		"trayRemindExpiry":  "Days of access left: %d. Renew in advance so the VPN keeps working. Click to open Renew.",
		"trayRemindTraffic": "%d%% of traffic left. Renew in advance so the VPN keeps working. Click to open Renew.",
		"sellerBadLink":     "the seller's link failed the check and was not opened",
		"logSellerBadLink":  "seller's link rejected: %v",
		"dnsBad":            "an IPv4 address like 1.1.1.1 is needed",
		"dnsLocal":          "this is a local network address: it is not visible from the tunnel",
		"logNamesSecure":    "name lookups go to %s over HTTPS: the node only sees a connection to the resolver",
		"logNamesPlain":     "name lookups go as plain DNS through the tunnel: the node can read them",
		"keyGone":           "this key is no longer in the list",
		"logKeyUsed":        "active key: %s",
		"logKeyRemoved":     "key %s removed",
	},
}

// sayf собирает сообщение с подстановками.
//
// sprintf вынесен в переменную намеренно. Строка формата приходит из словаря
// выше, а не из кода, и go vet справедливо требует постоянного формата:
// подставить туда чужое значение — известная дыра. Здесь формат свой, лежит
// в таблице десятью строками выше и меняется только вместе с ней, поэтому
// проверять действительно нечего. Обращение через переменную говорит это vet,
// а комментарий — тому, кто будет читать.
var sprintf = fmt.Sprintf

func sayf(key string, args ...any) string { return sprintf(say(key), args...) }
