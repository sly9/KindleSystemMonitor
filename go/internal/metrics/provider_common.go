package metrics

import (
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
)

func readCPUMem() (cpuPct *float64, memPct *float64) {
	if pcts, err := cpu.Percent(500*time.Millisecond, false); err == nil && len(pcts) > 0 {
		v := pcts[0]
		cpuPct = &v
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		v := vm.UsedPercent
		memPct = &v
	}
	return
}
