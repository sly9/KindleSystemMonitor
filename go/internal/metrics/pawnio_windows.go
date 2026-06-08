//go:build windows

package metrics

import (
	_ "embed"
	"sync"
	"syscall"
	"unsafe"
)

// AMDFamily17.bin is a Pawn module compiled for the PawnIO kernel driver.
// It exposes ioctl_read_smn(offset) and ioctl_read_msr(index), used here to
// read the CPU thermal-control register over the SMN bus. Sourced from
// LibreHardwareMonitor (MPL-2.0) which got it from PawnIO.Modules 0.1.6 (LGPL-2.1).
// See THIRD-PARTY-NOTICES.md.
//
//go:embed pawn/AMDFamily17.bin
var amdFamily17Bin []byte

const (
	// F17H_M01H_THM_TCON_CUR_TMP — same register from Zen 1 (Family 17h) all
	// the way through Zen 5 (Family 1Ah).
	smnThmTconCurTMP = 0x00059800

	tempRangeSelMask = 0x000C0000 // F17H_TEMP_RANGE_SEL_MASK
	tempTjSelMask    = 0x00030000 // F17H_TEMP_TJ_SEL_MASK
)

// PawnIOLib.dll ships with the PawnIO installer at this fixed path. We don't
// embed the DLL — it's a kernel-driver companion, the user must have PawnIO
// installed for any of this to work. See https://github.com/namazso/PawnIO.
var (
	pawnioDLL = syscall.NewLazyDLL(`C:\Program Files\PawnIO\PawnIOLib.dll`)

	procPawnIOOpen    = pawnioDLL.NewProc("pawnio_open")
	procPawnIOLoad    = pawnioDLL.NewProc("pawnio_load")
	procPawnIOExecute = pawnioDLL.NewProc("pawnio_execute")
	procPawnIOClose   = pawnioDLL.NewProc("pawnio_close")
)

// pawnReader holds a long-lived PawnIO executor with AMDFamily17.bin loaded.
type pawnReader struct {
	once   sync.Once
	mu     sync.Mutex
	handle uintptr
	ready  bool
	// lastErr is HRESULT from the latest failed init, exposed for doctor.
	lastErr uint32
}

func newPawnReader() *pawnReader { return &pawnReader{} }

// ensureInit lazily loads PawnIOLib, opens the driver, and loads our embedded
// Pawn module. Idempotent. Returns false if any step failed (e.g. PawnIO is
// not installed, or we're not running elevated).
func (p *pawnReader) ensureInit() bool {
	p.once.Do(func() {
		if err := pawnioDLL.Load(); err != nil {
			p.lastErr = 0x80070002 // ERROR_FILE_NOT_FOUND
			return
		}
		var h uintptr
		hr, _, _ := procPawnIOOpen.Call(uintptr(unsafe.Pointer(&h)))
		if hr != 0 {
			p.lastErr = uint32(hr)
			return
		}
		if len(amdFamily17Bin) == 0 {
			procPawnIOClose.Call(h)
			return
		}
		hr, _, _ = procPawnIOLoad.Call(
			h,
			uintptr(unsafe.Pointer(&amdFamily17Bin[0])),
			uintptr(len(amdFamily17Bin)),
		)
		if hr != 0 {
			p.lastErr = uint32(hr)
			procPawnIOClose.Call(h)
			return
		}
		p.handle = h
		p.ready = true
	})
	return p.ready
}

func (p *pawnReader) readSMN(offset uint32) (uint32, bool) {
	if !p.ensureInit() {
		return 0, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	in := [1]uint64{uint64(offset)}
	out := [1]uint64{}
	var retSize uintptr
	// pawnio_execute expects a null-terminated function name.
	name, _ := syscall.BytePtrFromString("ioctl_read_smn")
	hr, _, _ := procPawnIOExecute.Call(
		p.handle,
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(&in[0])), 1,
		uintptr(unsafe.Pointer(&out[0])), 1,
		uintptr(unsafe.Pointer(&retSize)),
	)
	if hr != 0 {
		return 0, false
	}
	return uint32(out[0]), true
}

// CPUTempC returns the current Tctl (or Tdie when the offset flag is set) in
// degrees Celsius, ported 1:1 from LibreHardwareMonitor's Amd17Cpu.cs L275-291.
func (p *pawnReader) CPUTempC() *float64 {
	raw, ok := p.readSMN(smnThmTconCurTMP)
	if !ok {
		return nil
	}
	offsetFlag := (raw&tempRangeSelMask) != 0 || (raw&tempTjSelMask) == tempTjSelMask
	t := float64((raw>>21)*125) / 1000.0
	if offsetFlag {
		t -= 49.0
	}
	return &t
}

// Status returns a doctor-friendly description (running ok / why it failed).
func (p *pawnReader) Status() (ready bool, detail string) {
	p.ensureInit()
	if p.ready {
		return true, "ok"
	}
	switch p.lastErr {
	case 0x80070002:
		return false, "PawnIOLib.dll not found (PawnIO not installed?) — install from https://github.com/namazso/PawnIO/releases"
	case 0x80070005:
		return false, "PawnIO open denied (HRESULT 0x80070005) — kindle-dash must run elevated"
	case 0:
		return false, "PawnIO init returned no error code but isn't ready"
	default:
		return false, "PawnIO init failed: HRESULT 0x" + uint32ToHex(p.lastErr)
	}
}

func uint32ToHex(v uint32) string {
	const hex = "0123456789abcdef"
	var b [8]byte
	for i := 7; i >= 0; i-- {
		b[i] = hex[v&0xf]
		v >>= 4
	}
	return string(b[:])
}
