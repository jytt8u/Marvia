<div align="center">

<img src="docs/shots/readme-brand.png" alt="Marvia" width="1000">

**Marvia — Android, Windows, панель и ноды. Разработка: 0.13.0-alpha.1.**

Выпуск 1.0 отложен. Основной опубликованный выпуск — историческая тестовая
v0.12.2; он не объявляется стабильным. Исправления в текущем коде сохранены.
[План доработки](docs/development-plan.md) · [Свой VPN с нуля](docs/start-from-zero.md)

[Русский](README.md) · [English](README.en.md) · [简体中文](README.zh-CN.md)

[![Android APK](https://img.shields.io/badge/ANDROID-APK-687482?style=for-the-badge&labelColor=30353b)](https://github.com/jytt8u/marvia/releases/download/v0.12.2/marvia-android.apk)
[![Windows test](https://img.shields.io/badge/WINDOWS-TEST-687482?style=for-the-badge&labelColor=30353b)](docs/start-from-zero.md#6-выпустить-доступ-себе-и-подключить-клиент)
[![Panel](https://img.shields.io/badge/PANEL-INSTALL-687482?style=for-the-badge&labelColor=30353b)](#установка-панели)
[![Docs](https://img.shields.io/badge/DOCS-GUIDE-687482?style=for-the-badge&labelColor=30353b)](docs/guide.md)

[Releases](https://github.com/jytt8u/marvia/releases) · [CI](https://github.com/jytt8u/marvia/actions) · [Privacy](docs/privacy.md)

</div>

## Приложение

Android · реальные снимки эмулятора версии 0.12.2 · без добавленного ключа

| Главная | Настройки VPN | Сеть и приватность |
|:---:|:---:|:---:|
| <img src="docs/shots/android-home.png" alt="Главная" width="260"> | <img src="docs/shots/android-settings.png" alt="Настройки VPN" width="260"> | <img src="docs/shots/android-advanced.png" alt="Сеть и приватность" width="260"> |

<details>
<summary>Windows и панель — сохранённые снимки</summary>

Windows 0.9.3 и локальный стенд панели. Это архивные снимки интерфейса.

<img src="docs/shots/windows.png" alt="Windows 0.9.3" width="1000">
<img src="docs/shots/panel-clients.png" alt="Marvia Partner" width="1000">

</details>

## Архитектура

<img src="docs/shots/readme-flow.svg" alt="Техническая схема Marvia: управление доступом отделено от пути VPN-пакетов" width="1200">

Панель управляет доступом; пакеты идут через ноду без панели.
[Границы доверия](docs/architecture.md).

## Защита VP1

<img src="docs/shots/readme-security-ru.svg" alt="Схема Noise внутри TLS и сравнение механизмов защиты VP1, WireGuard, VLESS и Trojan" width="1200">

**Отдельная Noise-сессия внутри TLS:** проверка ключа ноды и шифрование данных
не зависят только от внешнего TLS-канала. Это отличие архитектуры, а не
доказательство, что VP1 безопаснее всех остальных.
[Сравнение, источники и границы защиты](docs/security-comparison.md).

В списке доступа VP1 — **0 закрытых ключей клиентов**: ноде достаточно публичных.
[Автоматическая проверка 26.09.2026](docs/security-checks/2026-09-26/README.md):
0 достижимых известных уязвимостей после обновления зависимостей;
8,55 млн запусков четырёх fuzz-тестов без найденного сбоя. Это не независимый аудит.

## VP1 стал быстрее

<img src="docs/shots/readme-vp1-progress.svg" alt="VP1 до и после оптимизации: 435,0 → 678,3 МБ/с, +55,9% в локальном тесте; медианы и диапазоны пяти парных прогонов" width="1200">

**+56% к прежней версии VP1 в локальном тесте.** Меньше лишних записей TLS; шифрование и совместимость сохранены. Оптимизация сохранена в текущей разработке и не относится к старой сборке v0.12.2.

Пять парных прогонов по 512 МиБ на одном ПК, без интернета и TUN.
Это прирост локальной пропускной способности, не обещание ускорить интернет на 56%.

[До и после: данные](docs/benchmarks/2026-09-26-record-fit/README.md) ·
[Полное сравнение с VLESS и Trojan](docs/performance.md) · [Код теста](cmd/marvia-bench)
В полном локальном сравнении VLESS и Trojan пока быстрее VP1.

## Протоколы

WireGuard и OpenVPN передают IP-пакеты; остальные строки — прокси. На Android
Marvia создаёт системный VPN-интерфейс и для сторонних прокси-ключей.

| Протокол | Транспорт и отличие | Поддержка Marvia |
|---|---|---|
| **[VP1](docs/protocol.md)** | Noise-прокси поверх TLS/REALITY, WebSocket или QUIC; QUIC при недоступном UDP переходит на TCP | Своя нода, Android и Windows |
| [WireGuard](https://www.wireguard.com/protocol/) | IP-туннель через UDP; штатной маскировки под HTTPS нет | Android: сторонний ключ |
| [OpenVPN](https://openvpn.net/community-docs/community-articles/openvpn-2-7-manual.html) | IP-туннель через UDP или TCP с TLS; это отдельный протокол, а не обычный HTTPS | Не встроен |
| [VLESS + REALITY](https://xtls.github.io/en/config/transports/reality.html) | Прокси через TCP с маскировкой TLS под целевой сайт | Нода и Android |
| [Trojan](https://github.com/trojan-gfw/trojan/blob/master/docs/protocol.md) | Прокси внутри TLS с сайтом-прикрытием | Нода и Android |
| [Shadowsocks](https://shadowsocks.org/doc/what-is-shadowsocks.html) | Шифрованный TCP/UDP-прокси; сам по себе не выглядит как HTTPS | Android: сторонний ключ |
| [Hysteria 2](https://v2.hysteria.network/docs/developers/Protocol/) | Прокси через QUIC/UDP с видом HTTP/3; требуется доступный UDP | Android: сторонний ключ |

WireGuard, OpenVPN, Hysteria 2 и Xray в этом бенчмарке не запускались.
VP1 не совместим с клиентами WireGuard или Xray.

## Что внутри

| Клиент | Сервер |
|---|---|
| Android: свои и чужие ключи, выбор ноды, статистика, настройки VPN | Панель и ноды: лимиты, учёт, резервные копии, API бота |
| Windows: TUN-клиент | VP1, VLESS и Trojan; проверяемое обновление нод |

При ручном выборе страны приложение подключается к выбранной ноде без ожидания
замеров остальных; если она недоступна, ищет рабочую запасную.

## Чем отличается от других

| Проект | Основной фокус | Сильная сторона |
|---|---|---|
| **Marvia** | Панель + ноды + свои Android/Windows + VP1 | один комплект для покупателя и владельца |
| [Marzban](https://github.com/Gozargah/Marzban) | Xray-панель | периодические квоты, Telegram-бот; есть отдельный [Nabzram](https://github.com/Gozargah/Nabzram) |
| [Remnawave](https://docs.rw/) | Xray-панель и ноды | шаблоны Mihomo/sing-box, контроль устройств |
| [3x-ui](https://docs.sanaei.dev/docs/) | Xray-панель | широкий выбор протоколов и админ-инструментов |

Marvia пока уступает по автоматическому месячному сбросу квот, импорту клиентов
и форматам Clash/sing-box. Сравнение основано на документации проектов по
ссылкам в таблице; сравнимых замеров скорости конкурентов пока нет.

## Начать

> **Получили ключ?** Скачайте [Android APK](https://github.com/jytt8u/marvia/releases/download/v0.12.2/marvia-android.apk)
> или прочитайте [установку Windows](docs/start-from-zero.md#6-выпустить-доступ-себе-и-подключить-клиент),
> добавьте ссылку `marvia://…` в приложении и подключитесь.

Android также принимает VLESS, VMess, Trojan, Shadowsocks, Hysteria2 и WireGuard.
В Google Play приложения пока нет.

Публичная v0.12.2 предназначена для тестирования. Уже установленный исправленный
клиент не понижайте ради номера версии. Полный установщик Windows и новое APK
готовятся в alpha; их проверка на реальных устройствах ещё не завершена.

### Установка панели

Нужны Linux-сервер и домен с A-записью. Установщик берётся из
[тестового выпуска v0.12.2](https://github.com/jytt8u/marvia/releases/tag/v0.12.2).
Для одного VPS панель использует 8443, нода — 443. Полная подготовка и проверка
сумм описаны в [инструкции с нуля](docs/start-from-zero.md):

```bash
curl -fsSL https://github.com/jytt8u/marvia/releases/download/v0.12.2/install-panel.sh -o install-panel.sh
sudo sh install-panel.sh --domain panel.example.com --email you@example.com --port 8443 --from https://github.com/jytt8u/marvia/releases/download/v0.12.2/marvia_linux_amd64.tar.gz
```

Сохраните показанный при установке админский токен. Затем создайте ноду и
клиента в панели. [Полный порядок](docs/guide.md) · [API бота](docs/bot.md).

## Что пока не готово

- Google Play: проверка новой сборки на телефоне, декларации и тестирование. [План публикации](docs/google-play.md).
- Автоматический месячный сброс квоты и импорт покупателей с сохранением ссылок.
- iOS, подписки Clash/sing-box, TUIC, плагины Shadowsocks и раздельный отзыв
  устройств с общим ключом.

Для будущей 1.x планируется проверенная обратная совместимость API бота,
схемы `marvia://` и формата подписки. Сейчас действует стадия alpha.
Marvia не продаёт доступ, не принимает
платежи и не размещает серверы.

## Документы и сборка

[Руководство](docs/guide.md) ·
[Архитектура](docs/architecture.md) ·
[Протокол VP1](docs/protocol.md) ·
[Конфиденциальность](docs/privacy.md) ·
[История версий](CHANGELOG.md)

Для серверных бинарников нужен Go 1.26.6+:

```bash
go test ./...
go vet ./...
go build -o bin/ ./...
```

Android собирается отдельно через [`scripts/publish-apk.ps1`](scripts/publish-apk.ps1);
ключ подписи хранится вне репозитория.

## Лицензия

[AGPL-3.0](LICENSE). Пользоваться, изменять и раздавать можно свободно. Кто
раздаёт изменённую панель, ноду или приложение — в том числе даёт ими
пользоваться по сети, — открывает свои изменения под той же лицензией.
