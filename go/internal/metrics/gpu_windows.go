//go:build windows

package metrics

import (
	"sync"
	"syscall"
	"unsafe"
)

// We call nvml.dll directly via syscall instead of execing nvidia-smi.exe
// every interval. Same data source (nvidia-smi uses NVML internally), zero
// fork-exec cost, no console-window flash, no external binary on PATH.
//
// nvml.dll ships with the NVIDIA driver and sits at C:\Windows\System32\.
// On systems without an NVIDIA driver, DLL load fails and we return empty
// metrics — same graceful degrade as the previous nvidia-smi version.
var (
	nvmlDLL = syscall.NewLazyDLL("nvml.dll")

	procNvmlInit                      = nvmlDLL.NewProc("nvmlInit_v2")
	procNvmlDeviceGetCount            = nvmlDLL.NewProc("nvmlDeviceGetCount_v2")
	procNvmlDeviceGetHandleByIndex    = nvmlDLL.NewProc("nvmlDeviceGetHandleByIndex_v2")
	procNvmlDeviceGetUtilizationRates = nvmlDLL.NewProc("nvmlDeviceGetUtilizationRates")
	procNvmlDeviceGetMemoryInfo       = nvmlDLL.NewProc("nvmlDeviceGetMemoryInfo")
	procNvmlDeviceGetTemperature      = nvmlDLL.NewProc("nvmlDeviceGetTemperature")
)

const (
	nvmlSuccess        = 0
	nvmlTemperatureGPU = 0
)

// Match NVML's C struct layouts exactly.
type nvmlUtilization struct {
	GPU    uint32
	Memory uint32
}

type nvmlMemory struct {
	Total uint64
	Free  uint64
	Used  uint64
}

var (
	gpuOnce   sync.Once
	gpuReady  bool
	gpuHandle uintptr
)

// ensureNVML loads nvml.dll on first use, inits the library, grabs device 0.
// Idempotent + cheap after first call. Returns false (don't try further calls)
// if any step fails — caller silently degrades to empty gpuSample.
func ensureNVML() bool {
	gpuOnce.Do(func() {
		if err := nvmlDLL.Load(); err != nil {
			return // no NVIDIA driver on this box
		}
		if r, _, _ := procNvmlInit.Call(); r != nvmlSuccess {
			return
		}
		var count uint32
		if r, _, _ := procNvmlDeviceGetCount.Call(uintptr(unsafe.Pointer(&count))); r != nvmlSuccess || count == 0 {
			return
		}
		var h uintptr
		if r, _, _ := procNvmlDeviceGetHandleByIndex.Call(0, uintptr(unsafe.Pointer(&h))); r != nvmlSuccess {
			return
		}
		gpuHandle = h
		gpuReady = true
	})
	return gpuReady
}

func readGPU() gpuSample {
	var s gpuSample
	if !ensureNVML() {
		return s
	}

	var util nvmlUtilization
	if r, _, _ := procNvmlDeviceGetUtilizationRates.Call(
		gpuHandle, uintptr(unsafe.Pointer(&util)),
	); r == nvmlSuccess {
		v := float64(util.GPU)
		s.Util = &v
	}

	var mem nvmlMemory
	if r, _, _ := procNvmlDeviceGetMemoryInfo.Call(
		gpuHandle, uintptr(unsafe.Pointer(&mem)),
	); r == nvmlSuccess && mem.Total > 0 {
		v := float64(mem.Used) / float64(mem.Total) * 100
		s.Mem = &v
	}

	var temp uint32
	if r, _, _ := procNvmlDeviceGetTemperature.Call(
		gpuHandle, uintptr(nvmlTemperatureGPU), uintptr(unsafe.Pointer(&temp)),
	); r == nvmlSuccess {
		v := float64(temp)
		s.Temp = &v
	}

	return s
}
