# Что видит продавец и чего не видит

Обновлено 5 октября 2026 года. English version below.

Marvia — это программы, а не сервис. Панель и ноды ставит у себя тот, кто
раздаёт или продаёт доступ (дальше — продавец), приложение ставит тот, кто
подключается (покупатель). Единого сервера Marvia, куда что-то уходило бы,
нет: автор проекта о покупателях не знает ничего.

Эта страница описывает, что знает о покупателе код Marvia на каждом из трёх
мест, зачем и сколько это хранится. Каждое обещание здесь проверяется тестом
(список — в конце), и правка, которая его нарушит, на этих тестах краснеет.

**Чего эта страница не обещает.** Это обещания кода, а не продавца. Владелец
сервера управляет им целиком: он может поставить изменённую сборку, включить
журнал у обратного прокси перед панелью или снимать трафик средствами
системы — снаружи этого не проверить. Полной анонимности VPN не даёт вообще;
что именно он не защищает — ниже. Это техническое описание, а не юридическая
оценка.

## Панель

Панель знает, кому и на каких условиях выдан доступ. Через неё не идёт ни
байта трафика покупателя.

| Что | Зачем | Сколько хранится |
|---|---|---|
| Метка покупателя (`label`) | Продавцу — найти человека в списке; покупателю — название профиля в приложении. Хватит псевдонима: поле подсказывает это прямо | Пока покупатель не удалён |
| Ключ покупателя у продавца (`external_id`) | Боту продавца — не выдать второй доступ за одну оплату. Что в нём лежит, решает бот; пример бота кладёт Telegram ID | Пока покупатель не удалён |
| Срок, лимиты, тариф, токен подписки | Пускать или нет | Пока покупатель не удалён |
| Наборы доступа | VP1 — только публичный ключ: приватного панель не видит никогда. VLESS UUID и пароль Trojan — как есть: без них не собрать ссылку | Пока набор не отозван |
| Расход по каждой ноде, накопительный | Общая квота на все ноды: каждая нода должна знать, сколько потрачено на других. Побочный эффект — видно, на каких нодах покупатель бывал. Дат в этой таблице нет | Пока покупатель не удалён |
| Время последней связи | «Он вообще подключался?» — первый вопрос в поддержке. Одно время на покупателя, без ноды | Перезаписывается |
| Кто на связи сейчас: соединений и **число** адресов за час | Продавцу — видеть раздачу ключа. Только по живым нодам; ушедший с ноды исчезает из таблицы | Пока на связи |
| Отчёты приложений о нодах: нода, успех, задержка, чей отчёт | Поставить в подписке выше ноды, до которых люди доходят. «Чей» — чтобы один человек не накрутил жалобы количеством | 6 часов |
| Журнал действий продавца: кто (админ или имя ключа бота), что сделал, с каким покупателем | Спор «я платил» | 90 дней, удаляется вместе с покупателем |
| Сохранённые ответы на повторы оплаты | Повтор уведомления не продлевает дважды. Это карточка покупателя, как её видит бот, — без приватного ключа VP1 | 7 дней, удаляются вместе с покупателем |
| Посуточный расход по нодам | Графики. **Людей в нём нет**: день, нода, объём | 400 дней |

Чего в панели нет: IP-адресов покупателей, их устройств, почты, телефонов,
истории подключений, списка сайтов. Почту и телефон панель не спрашивает и
принять не может — в API для них нет полей.

Сетевой адрес приложения панель при этом **видит** — как любой сервер видит,
кто к нему пришёл за подпиской или с отчётом. Она его не записывает: ни в
базу, ни в журнал (сорванные TLS-рукопожатия, обрывы — журнал пишет их с
пометкой `<адрес>` вместо адреса). Если перед панелью стоит nginx, Caddy или
Cloudflare, их журналы — отдельная история, и настраивает её продавец.

Удаление покупателя уносит всё про него: наборы доступа, расход, время связи,
отчёты, записи журнала и сохранённые ответы. Остаётся только посуточный
расход нод — в нём покупателя и не было.

Копии базы содержат то же самое. Они шифруются, если продавец задал
`-backup-key`; хранится 7 последних.

Уборка по срокам идёт сама, раз в шесть часов и при запуске, а не только
после перезапуска панели.

## Нода

Нода знает, кого пускать, и пропускает трафик. Кто этот человек, она не знает.

| Что | Зачем | Где и сколько |
|---|---|---|
| Ключи доступа, лимиты и номер аккаунта — **без имён** | Пускать или нет | В памяти; зашифрованный снимок на диске на случай перезапуска без панели — не дольше 24 часов |
| Расход по номеру аккаунта | Квота | Файл расхода на диске |
| Число соединений аккаунта | Лимит соединений | В памяти |
| IP-адреса подключений | Лимит устройств: «один купил — раздал десятерым» иначе не увидеть | **Только у тех, кому лимит задан**, только в памяти, только час. Снятый лимит снимает адреса сразу. Наружу — в файл, в панель — уходит одно число |
| Адрес назначения | Без него трафик не доставить | Пока открыто соединение |

Раз в 15 секунд нода отправляет панели: расход по номерам аккаунтов, число
соединений и число адресов, свою версию и имена прикрытия. Больше ничего.

Журнал ноды не содержит ни адресов покупателей, ни адресов назначения, ни
ключей: ошибки сети, несущие адрес внутри себя, пишутся одной причиной
(«connect: connection refused»), сайт-прикрытие не пишет ничего.

## Что технически видит любая выходная нода

Это не про Marvia, а про любой VPN. Трафик выходит в интернет с ноды, и её
владелец может посмотреть на него средствами системы, минуя код Marvia:

- **видно:** адрес, с которого пришёл покупатель; к каким адресам и портам он
  ходит; имена сайтов (приложение часто передаёт цель именем, плюс SNI в начале
  HTTPS); когда и сколько. Запросы имён к Cloudflare, Google, Quad9 и AdGuard
  по умолчанию идут через туннель по HTTPS, и нода видит только соединение с
  резолвером; со своим адресом резолвера или с выключенным шифрованием имён
  нода видит и их;
- **не видно:** содержимое HTTPS — страницы, переписка, пароли, — и
  содержимое любых других зашифрованных соединений;
- **видно целиком:** всё, что идёт без шифрования, — сайты по `http://`,
  старые протоколы.

Код Marvia ничего из этого не записывает. Но выбирая продавца, вы выбираете,
кому доверить то, что видно из этого списка.

## Приложение

Приложение отправляет панели:

- запрос подписки по токену — за списком нод, сроком и квотой;
- отчёт о нодах после подключения: **номер ноды, успех и задержку** — ни
  адресов, ни текста ошибок, ни данных устройства. Отчёты выключаются: на
  Android — в дополнительных настройках VPN, в Windows — в настройках.

Ноде приложение предъявляет ключ и отправляет трафик. Страну выхода
приложение иногда спрашивает у `ipapi.co`, когда продавец её не указал, —
через ноду, так что сервис видит адрес ноды, а не телефона.

### На устройстве

Windows хранит дневные объёмы трафика по часам локально в
`%APPDATA%\Marvia\traffic.json`. В этом файле нет сайтов, адресов нод,
ключей или списка приложений; статистика не отправляется никуда.

Приложение хранит ключи и адреса подписок, список нод и их замеры, настройки
VPN и темы, выбор приложений для обхода туннеля, локальный журнал, расход и
историю сессий. Ключ VP1 содержит закрытый ключ и остаётся в хранилище
приложения: подтверждение доступа не требует отправлять его панели или ноде.
Резервное копирование данных приложения средствами Android выключено.
«Сбросить всё» удаляет настройки, ключи, кэши подписок и учёт трафика;
история сессий и диагностические файлы остаются до удаления приложения.

Приложение не содержит рекламных и аналитических SDK. Локальный журнал и
история сессий никуда автоматически не отправляются.

### Разрешения Android

`VpnService` нужен для маршрутизации трафика через туннель. Интернет — для
подписки и соединения с нодой. Уведомление и служба на переднем плане
показывают состояние VPN при погашенном экране. Запуск после перезагрузки
работает, если вы его включили. Список установленных приложений нужен для
обхода туннеля и остаётся на устройстве.

## Чего VPN не защищает вообще

- **Вход в аккаунты.** Сайт, где вы вошли, знает, что это вы, с любого адреса.
- **Отпечаток браузера, cookies, рекламные идентификаторы.** Сайты узнают
  устройство и без IP-адреса.
- **Само устройство.** Вредоносная программа на телефоне видит всё до
  туннеля.
- **Обход туннеля.** Приложения и маршруты, отмеченные для обхода, и DNS,
  если он настроен мимо туннеля, идут напрямую и открывают ваш адрес.
- **Оплату.** Продавец и платёжная система знают, кто заплатил; бот продавца
  знает ваш аккаунт в мессенджере. Панель Marvia этого не хранит, но бот и
  платёжка — не Marvia.
- **Сопоставление по времени.** Тот, кто видит и вход в туннель, и выход из
  него, может связать их по времени и объёму.
- **Сам факт VPN.** Маскировка затрудняет распознавание, но не гарантирует,
  что его не распознают.

## Как это проверить

Обещания этой страницы закреплены тестами:

| Обещание | Тест |
|---|---|
| В базе панели нет столбца для адреса, устройства, почты, телефона | `internal/panel/privacy_test.go`: `TestPanelSchemaHasNoPlaceForBuyerAddresses` |
| Адрес покупателя не оседает ни в базе, ни в журнале панели | `TestBuyerAddressReachesNeitherDatabaseNorJournal` |
| Отчёт о нодах хранит только ноду, успех, задержку | `TestAvailabilityReportStoresOnlyNodeSuccessAndLatency`, `internal/client`: `TestReportCarriesOnlyNodeSuccessAndLatency` |
| Отчёты живут 6 часов, уборка идёт без перезапуска | `TestAvailabilityReportsAreForgottenAfterTheWindow`, `TestOldRecordsAreForgottenWithoutRestart` |
| Панель не помнит, на какой ноде покупатель был когда | `TestPanelForgetsWhichNodeABuyerLeft`, `TestUsageKeepsNoTimeOfLastUseOnANode` |
| Удаление покупателя уносит сохранённые ответы | `TestDeletedUserLeavesNoRememberedResponse` |
| Нода держит адреса только ради лимита, только в памяти, только час | `internal/users/privacy_test.go` |
| Журналы панели и ноды не получают адрес, цель, токен, метку | `internal/redact/journal_test.go`, `cmd/marvia-node/serve_test.go`, `cmd/marvia-panel/server_test.go`, `internal/transport/ws_journal_test.go`, `internal/fallback/fallback_test.go` |
| В посуточной истории нет людей | `internal/panel/usage_test.go`: `TestDailyHistoryKeepsNoOneCompany` |
| Поле имени предлагает псевдоним, почту и телефон панель не спрашивает | `internal/panel/web_test.go`: `TestBuyerNameFieldSuggestsAPseudonym` |

По вопросам о приложении — [issue в проекте](https://github.com/jytt8u/marvia/issues).
По вопросам о данных на сервере — к тому, кто выдал ключ доступа.

---

# What the seller sees and what they don't

Updated 5 October 2026.

Marvia is software, not a service. Whoever gives out or sells access (the
seller) runs the panel and nodes; whoever connects (the buyer) runs the app.
There is no central Marvia server: the project author knows nothing about
buyers.

This page describes what Marvia's code knows about a buyer at each of the
three places, why, and for how long. Every promise is backed by a test (see
the Russian section above for the list).

**What this page does not promise.** These are promises of the code, not of
the seller. A server operator fully controls their server: they can run a
modified build, enable logging on a reverse proxy in front of the panel, or
capture traffic with system tools — none of which is visible from outside.
A VPN does not provide complete anonymity. This is a technical description,
not legal advice.

## Panel

The panel knows who was given access and on what terms. No buyer traffic
passes through it.

- **Stored until the buyer is deleted:** the label (a nickname is enough; it
  also appears as the profile name in the buyer's app), the seller's key for
  the buyer (`external_id`, set by the seller's bot — the example bot uses the
  Telegram ID), expiry, limits, plan, subscription token, access credentials
  (for VP1 only the public key; VLESS UUID and Trojan password as is), and
  cumulative traffic per node (needed for a quota shared across nodes; it
  reveals which nodes the buyer has used, but holds no dates).
- **Overwritten:** a single last-seen time per buyer, without the node.
- **While connected only:** connection count and the **number** of addresses
  in the last hour, per live node.
- **6 hours:** app reports about nodes — node, success, latency, whose report.
- **90 days:** the seller's action log (who acted, what, on which buyer);
  deleted together with the buyer.
- **7 days:** stored replies for payment retries — the buyer card as the bot
  sees it, without the VP1 private key; deleted together with the buyer.
- **400 days:** daily traffic per node — **no buyers in it**.

The panel stores no buyer IP addresses, devices, emails, phone numbers,
connection history or websites. It cannot accept an email or phone number:
the API has no field for them. It does **see** the app's network address,
like any server sees its clients, but writes it neither to the database nor
to its log (net/http errors are logged with `<адрес>` in place of the
address). Logs of nginx, Caddy or Cloudflare in front of the panel are the
seller's own configuration. Database backups contain the same data; they are
encrypted when the seller sets `-backup-key`. Retention cleanup runs every
six hours, not only on restart.

## Node

The node knows whom to admit and forwards traffic. It doesn't know who the
person is.

- Access keys, limits and an account **number, without names** — in memory,
  plus an encrypted snapshot on disk valid for at most 24 hours.
- Traffic per account number — on disk.
- Connection source IP addresses — **only for accounts with a device limit,
  only in memory, only for an hour**; removing the limit drops them at once.
  Only their count leaves the node.
- Destination address — while the connection is open.

Every 15 seconds the node reports to the panel: traffic per account number,
connection count, number of addresses, its version and cover names. Its log
holds no buyer addresses, destinations or keys.

## What any exit node can technically see

This applies to any VPN. Traffic leaves to the internet from the node, and
its operator can observe it with system tools, bypassing Marvia's code:

- **visible:** the buyer's source address; destination addresses and ports;
  site names (the app often passes the destination by name, plus SNI at the
  start of HTTPS); timing and volume. Name lookups to Cloudflare, Google, Quad9
  and AdGuard go through the tunnel over HTTPS by default, so the node only
  sees a connection to the resolver; with a custom resolver address or name
  encryption turned off, the node sees the lookups too;
- **not visible:** HTTPS content — pages, messages, passwords — and the
  content of any other encrypted connection;
- **fully visible:** anything unencrypted, such as `http://` sites.

## App

The app sends the panel a subscription request by token, and after
connecting a node report with **node number, success and latency only**.
Reports can be turned off (Android: advanced VPN settings; Windows: settings).
Exit-country detection sometimes queries `ipapi.co` through the node, so the
service sees the node's address, not the phone's.

On the device the app stores access keys and subscription URLs, node lists
and measurements, VPN and appearance settings, apps that bypass the VPN,
local logs, usage and session history. Windows keeps hourly traffic totals in
`%APPDATA%\Marvia\traffic.json` with no websites, node addresses, keys or app
list. A VP1 private key never leaves the app. Android backup of app data is
disabled. “Reset everything” removes settings, keys, subscription caches and
traffic totals; session history and diagnostic files remain until uninstall.
The app has no advertising or analytics SDK and sends no logs anywhere.

Android permissions: `VpnService` routes traffic into the tunnel; Internet
fetches subscriptions and connects to nodes; the foreground service shows VPN
status; start after reboot is optional; the installed-app list is used for
VPN bypass and stays on the device.

## What a VPN does not protect at all

Accounts you sign in to; browser fingerprints, cookies and ad identifiers;
malware on the device; apps, routes and DNS excluded from the tunnel; payment
(the seller, payment provider and the seller's bot know who paid); timing
correlation by someone who sees both ends of the tunnel; and the fact that a
VPN is in use may still be detected.

For questions about the app, open a
[project issue](https://github.com/jytt8u/marvia/issues). For data held on a
server, contact whoever supplied your access key.
