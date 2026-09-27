package vp1

import (
	"errors"
	"net"

	"github.com/flynn/noise"
)

// FastPrologue отделяет экспериментальный VP1 Fast от обычного VP1.
// Автосогласования и отката нет: стороны обязаны явно выбрать один протокол.
// Исходный Prologue остаётся прежним для совместимости установленных нод.
const FastPrologue = "marvia/vp1-fast/experimental-1"

// AES-256-GCM берём из Noise, который использует стандартную crypto/aes.
// Выигрыш зависит от аппаратного ускорения AES; на других CPU Fast может
// оказаться медленнее. Формат кадров, добивка и проверка тегов общие с VP1.
var fastCipherSuite = noise.NewCipherSuite(noise.DH25519, noise.CipherAESGCM, noise.HashBLAKE2s)

// ClientFastHandshake — экспериментальный Noise IK с AES-256-GCM.
// Вызывающий обеспечивает внешний TLS, как и для обычного ClientHandshake.
// В подписки и штатный выбор протокола Fast пока не включён.
func ClientFastHandshake(conn net.Conn, static KeyPair, serverPub []byte) (*Conn, error) {
	return clientHandshakeWith(conn, static, serverPub, fastCipherSuite, FastPrologue)
}

// ServerFastHandshake принимает только явно выбранный экспериментальный
// протокол. Список доступа и общая память повторов обязательны: их нельзя
// незаметно отключить пустым аргументом при создании нового слушателя.
func ServerFastHandshake(conn net.Conn, static KeyPair, guard *ReplayGuard, allow Authorizer) (*Conn, []byte, error) {
	if guard == nil || allow == nil {
		return nil, nil, errors.New("vp1 fast требует проверку ключей и защиту от повторов")
	}
	return serverHandshakeWith(conn, static, guard, allow, fastCipherSuite, FastPrologue)
}
