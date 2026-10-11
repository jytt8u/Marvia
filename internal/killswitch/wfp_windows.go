//go:build windows && (amd64 || arm64)

package killswitch

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Guard — правила, стоящие в системе. Живут, пока жив Guard и наш процесс.
//
// Сессия WFP динамическая: система снимает её фильтры сама, когда закрывается
// дескриптор, — в том числе когда процесс убит или упал. Это решение, а не
// удобство. Постоянные фильтры пережили бы падение и оставили компьютер без
// сети до переустановки программы, а человек не знал бы даже, что искать.
// Цена — после падения Marvia трафик идёт напрямую, как без kill switch.
type Guard struct {
	engine uintptr
}

// Start ставит правила одной транзакцией: либо все, либо ни одного.
// Половина набора — например, запрет без разрешения адаптера — означала бы
// компьютер без сети при живом туннеле.
func Start(rules []Rule) (*Guard, error) {
	app, err := ownAppID()
	if err != nil {
		return nil, fmt.Errorf("кто такой наш процесс для фильтра: %w", err)
	}
	defer fwpmFreeMemory0(unsafe.Pointer(&app))

	name, _ := windows.UTF16PtrFromString("Marvia kill switch")
	session := fwpmSession0{
		displayData:          fwpmDisplayData0{name: name, description: name},
		flags:                fwpmSessionFlagDynamic,
		txnWaitTimeoutInMSec: windows.INFINITE,
	}
	var engine uintptr
	if err := fwpCall(procFwpmEngineOpen0, 0, rpcCAuthnWinNT, 0, uintptr(unsafe.Pointer(&session)), uintptr(unsafe.Pointer(&engine))); err != nil {
		return nil, fmt.Errorf("служба фильтрации Windows (BFE) не ответила: %w", err)
	}
	if err := fwpCall(procFwpmTransactionBegin0, engine, 0); err != nil {
		_ = fwpCall(procFwpmEngineClose0, engine)
		return nil, err
	}
	if err := install(engine, rules, app); err != nil {
		_ = fwpCall(procFwpmTransactionAbort0, engine)
		_ = fwpCall(procFwpmEngineClose0, engine)
		return nil, err
	}
	if err := fwpCall(procFwpmTransactionCommit0, engine); err != nil {
		_ = fwpCall(procFwpmTransactionAbort0, engine)
		_ = fwpCall(procFwpmEngineClose0, engine)
		return nil, err
	}
	return &Guard{engine: engine}, nil
}

// Close снимает правила. Повторный вызов ничего не делает.
func (g *Guard) Close() error {
	if g == nil || g.engine == 0 {
		return nil
	}
	err := fwpCall(procFwpmEngineClose0, g.engine)
	g.engine = 0
	return err
}

// install кладёт подслой и фильтры в открытую транзакцию.
//
// Свой подслой с наибольшим весом: внутри него решает наш порядок правил, а
// чужие подслои (брандмауэр Windows, антивирус) решают своё отдельно. Запрет
// любого подслоя сильнее разрешения другого, поэтому наш общий запрет чужие
// разрешения не отменят; а неотменяемое разрешение своего процесса не
// отменят чужие запреты.
func install(engine uintptr, rules []Rule, app *fwpByteBlob) error {
	subLayer, err := windows.GenerateGUID()
	if err != nil {
		return err
	}
	name, _ := windows.UTF16PtrFromString("Marvia kill switch")
	sl := fwpmSublayer0{
		subLayerKey: subLayer,
		displayData: fwpmDisplayData0{name: name, description: name},
		weight:      0xffff,
	}
	if err := fwpCall(procFwpmSubLayerAdd0, engine, uintptr(unsafe.Pointer(&sl)), 0); err != nil {
		return fmt.Errorf("подслой фильтров: %w", err)
	}
	for _, r := range rules {
		if err := addFilter(engine, subLayer, r, app); err != nil {
			return fmt.Errorf("правило %q: %w", r.Name, err)
		}
	}
	return nil
}

// addFilter переводит одно правило на язык WFP.
//
// Значения условий лежат в куче и держатся в keep до конца вызова: WFP видит
// их по адресам, спрятанным в uintptr, а такой адрес сборщик мусора за ссылку
// не считает. Стек для них не годится: при росте он переезжает, и адрес в
// uintptr остался бы старым.
func addFilter(engine uintptr, subLayer windows.GUID, r Rule, app *fwpByteBlob) error {
	var conds []fwpmFilterCondition0
	var keep []any
	add := func(field windows.GUID, match uint32, typ uint32, value uintptr) {
		conds = append(conds, fwpmFilterCondition0{fieldKey: field, matchType: match, conditionValue: fwpValue0{typ: typ, value: value}})
	}

	if r.ThisApp {
		add(conditionALEAppID, fwpMatchEqual, fwpByteBlobType, uintptr(unsafe.Pointer(app)))
	}
	if r.Loopback {
		add(conditionFlags, fwpMatchFlagsAllSet, fwpUint32, fwpConditionFlagIsLoopback)
	}
	if r.Interface != 0 {
		luid := new(uint64)
		*luid = r.Interface
		keep = append(keep, luid)
		add(conditionIPLocalInterface, fwpMatchEqual, fwpUint64, uintptr(unsafe.Pointer(luid)))
	}
	if r.Proto != 0 {
		add(conditionIPProtocol, fwpMatchEqual, fwpUint8, uintptr(r.Proto))
	}
	// Тип ICMP в WFP — то же поле, что локальный порт: так система
	// раскладывает ICMP по уровням ALE.
	if r.Proto == ProtoICMPv6 && r.ICMPType != 0 {
		add(conditionIPLocalPort, fwpMatchEqual, fwpUint16, uintptr(r.ICMPType))
	}
	if r.LocalPort != 0 {
		add(conditionIPLocalPort, fwpMatchEqual, fwpUint16, uintptr(r.LocalPort))
	}
	if r.RemotePort != 0 {
		add(conditionIPRemotePort, fwpMatchEqual, fwpUint16, uintptr(r.RemotePort))
	}
	if r.Local.IsValid() {
		typ, value, err := prefixValue(r.Local, &keep)
		if err != nil {
			return err
		}
		add(conditionIPLocalAddress, fwpMatchEqual, typ, value)
	}
	// Повтор одного поля WFP складывает через «или» — весь список адресов
	// помещается в один фильтр.
	for _, p := range r.Remote {
		typ, value, err := prefixValue(p, &keep)
		if err != nil {
			return err
		}
		add(conditionIPRemoteAddress, fwpMatchEqual, typ, value)
	}

	name, _ := windows.UTF16PtrFromString(r.Name)
	f := &fwpmFilter0{
		displayData:         fwpmDisplayData0{name: name, description: name},
		layerKey:            layerOf(r),
		subLayerKey:         subLayer,
		weight:              fwpValue0{typ: fwpUint8, value: uintptr(r.Weight)},
		numFilterConditions: uint32(len(conds)),
		action:              fwpmAction0{typ: fwpActionBlock},
	}
	if len(conds) > 0 {
		f.filterCondition = &conds[0]
	}
	if r.Permit {
		f.action.typ = fwpActionPermit
	}
	if r.Hard {
		f.flags = fwpmFilterFlagClearActionRight
	}
	var id uint64
	err := fwpCall(procFwpmFilterAdd0, engine, uintptr(unsafe.Pointer(f)), 0, uintptr(unsafe.Pointer(&id)))
	runtime.KeepAlive(keep)
	runtime.KeepAlive(conds)
	runtime.KeepAlive(f)
	runtime.KeepAlive(name)
	return err
}

func layerOf(r Rule) windows.GUID {
	switch {
	case r.Out && !r.V6:
		return layerALEAuthConnectV4
	case r.Out:
		return layerALEAuthConnectV6
	case !r.V6:
		return layerALEAuthRecvAcceptV4
	default:
		return layerALEAuthRecvAcceptV6
	}
}

// prefixValue — подсеть как значение условия. Адрес IPv4 в WFP — число в
// порядке байтов машины, а не сети.
func prefixValue(p netip.Prefix, keep *[]any) (uint32, uintptr, error) {
	if !p.IsValid() {
		return 0, 0, errors.New("пустая подсеть")
	}
	p = p.Masked()
	if p.Addr().Is4() {
		a := p.Addr().As4()
		v := &fwpV4AddrAndMask{
			addr: uint32(a[0])<<24 | uint32(a[1])<<16 | uint32(a[2])<<8 | uint32(a[3]),
		}
		if p.Bits() > 0 {
			v.mask = ^uint32(0) << (32 - p.Bits())
		}
		*keep = append(*keep, v)
		return fwpV4AddrMask, uintptr(unsafe.Pointer(v)), nil
	}
	v := &fwpV6AddrAndMask{addr: p.Addr().As16(), prefixLength: uint8(p.Bits())}
	*keep = append(*keep, v)
	return fwpV6AddrMask, uintptr(unsafe.Pointer(v)), nil
}

// ownAppID — наш исполняемый файл так, как его знает WFP. Освобождает
// вызывающий через fwpmFreeMemory0.
func ownAppID() (*fwpByteBlob, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	path, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return nil, err
	}
	var app *fwpByteBlob
	if err := fwpCall(procFwpmGetAppIdFromFileName0, uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&app))); err != nil {
		return nil, err
	}
	return app, nil
}

var (
	fwpuclnt = windows.NewLazySystemDLL("fwpuclnt.dll")

	procFwpmEngineOpen0           = fwpuclnt.NewProc("FwpmEngineOpen0")
	procFwpmEngineClose0          = fwpuclnt.NewProc("FwpmEngineClose0")
	procFwpmTransactionBegin0     = fwpuclnt.NewProc("FwpmTransactionBegin0")
	procFwpmTransactionCommit0    = fwpuclnt.NewProc("FwpmTransactionCommit0")
	procFwpmTransactionAbort0     = fwpuclnt.NewProc("FwpmTransactionAbort0")
	procFwpmSubLayerAdd0          = fwpuclnt.NewProc("FwpmSubLayerAdd0")
	procFwpmFilterAdd0            = fwpuclnt.NewProc("FwpmFilterAdd0")
	procFwpmGetAppIdFromFileName0 = fwpuclnt.NewProc("FwpmGetAppIdFromFileName0")
	procFwpmFreeMemory0           = fwpuclnt.NewProc("FwpmFreeMemory0")
)

// fwpCall зовёт функцию WFP. Они возвращают код ошибки прямо, а не через
// GetLastError.
func fwpCall(proc *windows.LazyProc, args ...uintptr) error {
	if err := proc.Find(); err != nil {
		return err
	}
	r, _, _ := syscall.SyscallN(proc.Addr(), args...)
	if r != 0 {
		return syscall.Errno(r)
	}
	return nil
}

func fwpmFreeMemory0(p unsafe.Pointer) {
	if procFwpmFreeMemory0.Find() == nil {
		_, _, _ = syscall.SyscallN(procFwpmFreeMemory0.Addr(), uintptr(p))
	}
}
