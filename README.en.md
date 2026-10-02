<div align="center">

<img src="docs/shots/readme-brand.png" alt="Marvia" width="1000">

**Your own VPN, end to end: protocol, nodes, panel, and apps for Android and Windows.**

Whoever hands out access installs the panel with one script and adds each node
with a line copied from the panel. Whoever connects gets a single link: the app
picks a node by itself and, if it stops responding, looks for a working one.

[Русский](README.md) · [English](README.en.md) · [简体中文](README.zh-CN.md)

[![Android APK](https://img.shields.io/badge/ANDROID-APK-687482?style=for-the-badge&labelColor=30353b)](https://github.com/jytt8u/marvia/releases/download/v0.12.2/marvia-android.apk)
[![Windows test](https://img.shields.io/badge/WINDOWS-TEST-687482?style=for-the-badge&labelColor=30353b)](docs/start-from-zero.md#6-выпустить-доступ-себе-и-подключить-клиент)
[![Panel](https://img.shields.io/badge/PANEL-INSTALL-687482?style=for-the-badge&labelColor=30353b)](#install-the-panel)
[![Docs](https://img.shields.io/badge/DOCS-GUIDE-687482?style=for-the-badge&labelColor=30353b)](docs/guide.md)

**Stage: alpha, `0.13.0-alpha.1`.** 1.0 is postponed until it is verified on
real devices and networks — [criteria (Russian)](docs/development-plan.md).
The published v0.12.2 is a test build and is not declared stable.

[Your VPN from scratch (Russian)](docs/start-from-zero.md) · [Releases](https://github.com/jytt8u/marvia/releases) · [CI](https://github.com/jytt8u/marvia/actions) · [Privacy](docs/privacy.md)

</div>

## The app

Android · actual emulator captures from 0.12.2 · no key added

| Home | VPN settings | Network and privacy |
|:---:|:---:|:---:|
| <img src="docs/shots/android-home.png" alt="Home" width="260"> | <img src="docs/shots/android-settings.png" alt="VPN settings" width="260"> | <img src="docs/shots/android-advanced.png" alt="Network and privacy" width="260"> |

<details>
<summary>Windows and panel — archived captures</summary>

Windows 0.9.3 and a local panel instance. These are archived UI captures.

<img src="docs/shots/windows.png" alt="Windows 0.9.3" width="1000">
<img src="docs/shots/panel-clients.png" alt="Marvia Partner" width="1000">

</details>

## Architecture

<img src="docs/shots/readme-flow.svg" alt="Technical diagram: access control is separate from the VPN data path" width="1200">

The panel grants access; VPN packets pass through a node, not the panel.
[Trust boundaries](docs/architecture.md).

## VP1 security

<img src="docs/shots/readme-security-en.svg" alt="Noise inside TLS, with a comparison of VP1, WireGuard, VLESS and Trojan security mechanisms" width="1200">

**A separate Noise session inside TLS:** node-key verification and payload
encryption do not rely solely on the outer TLS channel. This is an architectural
distinction, not proof that VP1 is safer than every alternative.
[Comparison, sources and protection boundaries](docs/security-comparison.md).

VP1 access records contain **0 client private keys**; nodes only need public keys.
[Automated checks, 26 Sep 2026](docs/security-checks/2026-09-26/README.md):
0 reachable known vulnerabilities after dependency updates;
8.55 million executions across four fuzz targets with no failure found. This is not an independent audit.

## A faster VP1

<img src="docs/shots/readme-vp1-progress.svg" alt="VP1 before and after optimization: 435.0 → 678.3 MB/s, +55.9% in a local test; medians and ranges of five paired runs" width="1200">

**56% more throughput than the previous VP1 build in a local test.** Fewer unnecessary TLS records; encryption and wire compatibility are unchanged. The optimization is retained in current development; this is not a measurement of the old v0.12.2 build.

Five paired 512 MiB runs on one PC, without Internet or TUN.
This is a local throughput gain, not a promise of 56% faster Internet.

[Before/after data](docs/benchmarks/2026-09-26-record-fit/README.md) ·
[Full comparison with VLESS and Trojan](docs/performance.md) · [Benchmark source](cmd/marvia-bench)
VLESS and Trojan remain faster than VP1 in the full local comparison.

## Protocols

WireGuard and OpenVPN tunnel IP packets; the others below are proxies. On
Android, Marvia also creates a system VPN interface for third-party proxy keys.

| Protocol | Transport and distinction | Marvia support |
|---|---|---|
| **[VP1](docs/protocol.md)** | Noise proxy over TLS/REALITY, WebSocket or QUIC; falls back to TCP if QUIC/UDP is unavailable | First-party node, Android and Windows |
| [WireGuard](https://www.wireguard.com/protocol/) | IP tunnel over UDP; no built-in HTTPS disguise | Android: third-party key |
| [OpenVPN](https://openvpn.net/community-docs/community-articles/openvpn-2-7-manual.html) | IP tunnel over UDP or TCP with TLS; a separate protocol, not ordinary HTTPS | Not integrated |
| [VLESS + REALITY](https://xtls.github.io/en/config/transports/reality.html) | TCP proxy with TLS handshake disguised as a target site | Node and Android |
| [Trojan](https://github.com/trojan-gfw/trojan/blob/master/docs/protocol.md) | Proxy inside TLS with a cover site | Node and Android |
| [Shadowsocks](https://shadowsocks.org/doc/what-is-shadowsocks.html) | Encrypted TCP/UDP proxy; does not resemble HTTPS on its own | Android: third-party key |
| [Hysteria 2](https://v2.hysteria.network/docs/developers/Protocol/) | QUIC/UDP proxy with HTTP/3 appearance; requires working UDP | Android: third-party key |

WireGuard, OpenVPN, Hysteria 2 and Xray were not run in this benchmark.
VP1 is not compatible with WireGuard or Xray clients.

## What is included

| Client | Server |
|---|---|
| Android: Marvia and third-party keys, node selection, usage and VPN settings | Panel and nodes: limits, accounting, backups, bot API, migration from Marzban and 3x-ui |
| Windows: TUN client | VP1, VLESS and Trojan; verified node updates |

Manually selected nodes connect without waiting for measurements of every
other node. If the selected node is unavailable, the client checks fallbacks.

## Compared with other projects

| Project | Focus | Notable capability |
|---|---|---|
| **Marvia** | Panel + nodes + first-party Android/Windows + VP1 | One stack for users and operators |
| [Marzban](https://github.com/Gozargah/Marzban) | Xray panel | Scheduled quotas and Telegram integration |
| [Remnawave](https://docs.rw/) | Xray panel and nodes | Mihomo/sing-box templates, device controls |
| [3x-ui](https://docs.sanaei.dev/docs/) | Xray panel | Broad protocol and administration support |

Marvia still lacks automatic monthly quota resets and Clash/sing-box
subscriptions. Buyers move from Marzban and 3x-ui with their existing keys and
subscription addresses â [what changes on the way (Russian)](docs/guide.md#Ð¿ÐµÑÐµÐµÐ·Ð´-Ñ-marzban-Ð¸-3x-ui). The linked project documentation
supports the feature comparison; no matched speed ranking is available.

## Get started

> **Have a key?** Download the [Android APK](https://github.com/jytt8u/marvia/releases/download/v0.12.2/marvia-android.apk)
> or read the [Windows setup instructions](docs/start-from-zero.md#6-выпустить-доступ-себе-и-подключить-клиент),
> add a `marvia://…` link in the app, and connect.

Android also accepts VLESS, VMess, Trojan, Shadowsocks, Hysteria2 and WireGuard
links. The app is not yet on Google Play.

v0.12.2 is for testing. Do not downgrade an installed fixed client just for its
version number. The complete Windows installer and new APK are being prepared
in alpha; verification on real devices is not complete.

### Install the panel

Use a Linux server and a domain with an A record. Download the installer from
the [test release v0.12.2](https://github.com/jytt8u/marvia/releases/tag/v0.12.2).
On a single VPS, use 8443 for the panel and 443 for the node. See the
[full preparation and checksum instructions](docs/start-from-zero.md):

```bash
curl -fsSL https://github.com/jytt8u/marvia/releases/download/v0.12.2/install-panel.sh -o install-panel.sh
sudo sh install-panel.sh --domain panel.example.com --email you@example.com --port 8443 --from https://github.com/jytt8u/marvia/releases/download/v0.12.2/marvia_linux_amd64.tar.gz
```

Save the admin token displayed during installation, then create a node and a
client in the panel. [Full guide (Russian)](docs/guide.md) · [Bot API](docs/bot.md).

## Still to do

- Google Play: physical-device verification, declarations
  and testing. [Publication plan (Russian)](docs/google-play.md).
- Monthly quota resets. Migration from Marzban and 3x-ui does not carry over VMess,
  Shadowsocks, Vision links, or MySQL and PostgreSQL databases.
- iOS, Clash/sing-box subscriptions, TUIC, Shadowsocks plugins, and per-device
  revocation when a key is shared.

For a future verified 1.x release, the bot API, `marvia://` links and subscription format are intended to
remain backward compatible throughout 1.x. Marvia does not sell VPN access,
take payments or host servers.

## Docs and build

[Guide](docs/guide.md) · [Architecture](docs/architecture.md) ·
[VP1 protocol](docs/protocol.md) · [Privacy](docs/privacy.md) ·
[Changelog](CHANGELOG.md) · [Contributing (Russian)](CONTRIBUTING.md) ·
[Security](SECURITY.md)

Server binaries require Go 1.26.6 or newer:

```bash
go test ./...
go vet ./...
go build -o bin/ ./...
```

Android is built separately with [`scripts/publish-apk.ps1`](scripts/publish-apk.ps1);
the signing key is stored outside this repository.

## License

[AGPL-3.0](LICENSE). You are free to use, modify and share it. If you
distribute a modified panel, node or app — including letting people use it
over a network — you publish your changes under the same license.
