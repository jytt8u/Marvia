//go:build windows && (amd64 || arm64)

package killswitch

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Структуры fwpmtypes.h в раскладке 64-битной Windows. Ошибка в смещении
// здесь не даёт ошибки компиляции и редко даёт ошибку вызова: WFP молча
// прочитает не то поле. Поэтому ниже размеры и смещения сверены с
// заголовками (те же числа проверяет у себя wireguard-windows) и закреплены
// так, что сборка с другой раскладкой не пройдёт.

type fwpByteBlob struct {
	size uint32
	data *byte
}

// fwpValue0 — FWP_VALUE0 и FWP_CONDITION_VALUE0: тип и объединение, в котором
// числа до 32 бит лежат прямо, а всё прочее — указателем.
type fwpValue0 struct {
	typ   uint32
	value uintptr
}

type fwpmDisplayData0 struct {
	name        *uint16
	description *uint16
}

type fwpmFilterCondition0 struct {
	fieldKey       windows.GUID
	matchType      uint32
	conditionValue fwpValue0
}

type fwpmAction0 struct {
	typ        uint32
	filterType windows.GUID
}

type fwpmFilter0 struct {
	filterKey           windows.GUID
	displayData         fwpmDisplayData0
	flags               uint32
	providerKey         *windows.GUID
	providerData        fwpByteBlob
	layerKey            windows.GUID
	subLayerKey         windows.GUID
	weight              fwpValue0
	numFilterConditions uint32
	filterCondition     *fwpmFilterCondition0
	action              fwpmAction0
	// Следом в C — объединение с UINT64, выровненное на 8; в Go поле GUID
	// выровнялось бы на 4 и съехало.
	_                  [4]byte
	providerContextKey windows.GUID
	reserved           *windows.GUID
	filterID           uint64
	effectiveWeight    fwpValue0
}

type fwpmSession0 struct {
	sessionKey           windows.GUID
	displayData          fwpmDisplayData0
	flags                uint32
	txnWaitTimeoutInMSec uint32
	processID            uint32
	sid                  *windows.SID
	username             *uint16
	kernelMode           int32
}

type fwpmSublayer0 struct {
	subLayerKey  windows.GUID
	displayData  fwpmDisplayData0
	flags        uint32
	providerKey  *windows.GUID
	providerData fwpByteBlob
	weight       uint16
}

type fwpV4AddrAndMask struct {
	addr uint32
	mask uint32
}

type fwpV6AddrAndMask struct {
	addr         [16]byte
	prefixLength uint8
}

// Закреплённая раскладка: каждое выражение — 0 только при точном совпадении,
// а при несовпадении одна из разностей уходит в минус, и константа без знака
// переполняется — сборка не проходит.
const (
	_ uintptr = unsafe.Sizeof(fwpByteBlob{}) - 16 + (16 - unsafe.Sizeof(fwpByteBlob{}))
	_ uintptr = unsafe.Offsetof(fwpValue0{}.value) - 8 + (8 - unsafe.Offsetof(fwpValue0{}.value))

	_ uintptr = unsafe.Sizeof(fwpmFilterCondition0{}) - 40 + (40 - unsafe.Sizeof(fwpmFilterCondition0{}))
	_ uintptr = unsafe.Offsetof(fwpmFilterCondition0{}.matchType) - 16 + (16 - unsafe.Offsetof(fwpmFilterCondition0{}.matchType))
	_ uintptr = unsafe.Offsetof(fwpmFilterCondition0{}.conditionValue) - 24 + (24 - unsafe.Offsetof(fwpmFilterCondition0{}.conditionValue))

	_ uintptr = unsafe.Sizeof(fwpmAction0{}) - 20 + (20 - unsafe.Sizeof(fwpmAction0{}))

	_ uintptr = unsafe.Sizeof(fwpmFilter0{}) - 200 + (200 - unsafe.Sizeof(fwpmFilter0{}))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.displayData) - 16 + (16 - unsafe.Offsetof(fwpmFilter0{}.displayData))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.flags) - 32 + (32 - unsafe.Offsetof(fwpmFilter0{}.flags))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.providerKey) - 40 + (40 - unsafe.Offsetof(fwpmFilter0{}.providerKey))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.providerData) - 48 + (48 - unsafe.Offsetof(fwpmFilter0{}.providerData))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.layerKey) - 64 + (64 - unsafe.Offsetof(fwpmFilter0{}.layerKey))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.subLayerKey) - 80 + (80 - unsafe.Offsetof(fwpmFilter0{}.subLayerKey))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.weight) - 96 + (96 - unsafe.Offsetof(fwpmFilter0{}.weight))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.numFilterConditions) - 112 + (112 - unsafe.Offsetof(fwpmFilter0{}.numFilterConditions))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.filterCondition) - 120 + (120 - unsafe.Offsetof(fwpmFilter0{}.filterCondition))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.action) - 128 + (128 - unsafe.Offsetof(fwpmFilter0{}.action))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.providerContextKey) - 152 + (152 - unsafe.Offsetof(fwpmFilter0{}.providerContextKey))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.reserved) - 168 + (168 - unsafe.Offsetof(fwpmFilter0{}.reserved))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.filterID) - 176 + (176 - unsafe.Offsetof(fwpmFilter0{}.filterID))
	_ uintptr = unsafe.Offsetof(fwpmFilter0{}.effectiveWeight) - 184 + (184 - unsafe.Offsetof(fwpmFilter0{}.effectiveWeight))

	_ uintptr = unsafe.Sizeof(fwpmSession0{}) - 72 + (72 - unsafe.Sizeof(fwpmSession0{}))
	_ uintptr = unsafe.Offsetof(fwpmSession0{}.flags) - 32 + (32 - unsafe.Offsetof(fwpmSession0{}.flags))
	_ uintptr = unsafe.Offsetof(fwpmSession0{}.txnWaitTimeoutInMSec) - 36 + (36 - unsafe.Offsetof(fwpmSession0{}.txnWaitTimeoutInMSec))
	_ uintptr = unsafe.Offsetof(fwpmSession0{}.sid) - 48 + (48 - unsafe.Offsetof(fwpmSession0{}.sid))
	_ uintptr = unsafe.Offsetof(fwpmSession0{}.kernelMode) - 64 + (64 - unsafe.Offsetof(fwpmSession0{}.kernelMode))

	_ uintptr = unsafe.Sizeof(fwpmSublayer0{}) - 72 + (72 - unsafe.Sizeof(fwpmSublayer0{}))
	_ uintptr = unsafe.Offsetof(fwpmSublayer0{}.providerKey) - 40 + (40 - unsafe.Offsetof(fwpmSublayer0{}.providerKey))
	_ uintptr = unsafe.Offsetof(fwpmSublayer0{}.providerData) - 48 + (48 - unsafe.Offsetof(fwpmSublayer0{}.providerData))
	_ uintptr = unsafe.Offsetof(fwpmSublayer0{}.weight) - 64 + (64 - unsafe.Offsetof(fwpmSublayer0{}.weight))

	_ uintptr = unsafe.Sizeof(fwpV4AddrAndMask{}) - 8 + (8 - unsafe.Sizeof(fwpV4AddrAndMask{}))
	_ uintptr = unsafe.Sizeof(fwpV6AddrAndMask{}) - 17 + (17 - unsafe.Sizeof(fwpV6AddrAndMask{}))
)

// Значения из fwptypes.h и fwpmtypes.h.
const (
	fwpUint8        = 1
	fwpUint16       = 2
	fwpUint32       = 3
	fwpUint64       = 4
	fwpByteBlobType = 12
	fwpV4AddrMask   = 0x100
	fwpV6AddrMask   = 0x101

	fwpMatchEqual       = 0
	fwpMatchFlagsAllSet = 6

	fwpActionBlock  = 0x1001
	fwpActionPermit = 0x1002

	fwpmFilterFlagClearActionRight = 0x8
	fwpmSessionFlagDynamic         = 0x1
	fwpConditionFlagIsLoopback     = 0x1

	rpcCAuthnWinNT = 10
)

var (
	layerALEAuthConnectV4    = windows.GUID{Data1: 0xc38d57d1, Data2: 0x05a7, Data3: 0x4c33, Data4: [8]byte{0x90, 0x4f, 0x7f, 0xbc, 0xee, 0xe6, 0x0e, 0x82}}
	layerALEAuthConnectV6    = windows.GUID{Data1: 0x4a72393b, Data2: 0x319f, Data3: 0x44bc, Data4: [8]byte{0x84, 0xc3, 0xba, 0x54, 0xdc, 0xb3, 0xb6, 0xb4}}
	layerALEAuthRecvAcceptV4 = windows.GUID{Data1: 0xe1cd9fe7, Data2: 0xf4b5, Data3: 0x4273, Data4: [8]byte{0x96, 0xc0, 0x59, 0x2e, 0x48, 0x7b, 0x86, 0x50}}
	layerALEAuthRecvAcceptV6 = windows.GUID{Data1: 0xa3b42c97, Data2: 0x9f04, Data3: 0x4672, Data4: [8]byte{0xb8, 0x7e, 0xce, 0xe9, 0xc4, 0x83, 0x25, 0x7f}}

	conditionALEAppID         = windows.GUID{Data1: 0xd78e1e87, Data2: 0x8644, Data3: 0x4ea5, Data4: [8]byte{0x94, 0x37, 0xd8, 0x09, 0xec, 0xef, 0xc9, 0x71}}
	conditionFlags            = windows.GUID{Data1: 0x632ce23b, Data2: 0x5167, Data3: 0x435c, Data4: [8]byte{0x86, 0xd7, 0xe9, 0x03, 0x68, 0x4a, 0xa8, 0x0c}}
	conditionIPLocalInterface = windows.GUID{Data1: 0x4cd62a49, Data2: 0x59c3, Data3: 0x4969, Data4: [8]byte{0xb7, 0xf3, 0xbd, 0xa5, 0xd3, 0x28, 0x90, 0xa4}}
	conditionIPProtocol       = windows.GUID{Data1: 0x3971ef2b, Data2: 0x623e, Data3: 0x4f9a, Data4: [8]byte{0x8c, 0xb1, 0x6e, 0x79, 0xb8, 0x06, 0xb9, 0xa7}}
	conditionIPLocalPort      = windows.GUID{Data1: 0x0c1ba1af, Data2: 0x5765, Data3: 0x453f, Data4: [8]byte{0xaf, 0x22, 0xa8, 0xf7, 0x91, 0xac, 0x77, 0x5b}}
	conditionIPRemotePort     = windows.GUID{Data1: 0xc35a604d, Data2: 0xd22b, Data3: 0x4e1a, Data4: [8]byte{0x91, 0xb4, 0x68, 0xf6, 0x74, 0xee, 0x67, 0x4b}}
	conditionIPLocalAddress   = windows.GUID{Data1: 0xd9ee00de, Data2: 0xc1ef, Data3: 0x4617, Data4: [8]byte{0xbf, 0xe3, 0xff, 0xd8, 0xf5, 0xa0, 0x89, 0x57}}
	conditionIPRemoteAddress  = windows.GUID{Data1: 0xb235ae9a, Data2: 0x1d64, Data3: 0x49b8, Data4: [8]byte{0xa4, 0x4c, 0x5f, 0xf3, 0xd9, 0x09, 0x50, 0x45}}
)
