package metrics

import (
	"os/exec"
	"strconv"
	"strings"
)

func readGPU() gpuSample {
	var s gpuSample
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=utilization.gpu,memory.used,memory.total,temperature.gpu",
		"--format=csv,noheader,nounits",
	)
	cmd.SysProcAttr = noWindowAttr()
	out, err := cmd.Output()
	if err != nil {
		return s
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	parts := strings.Split(line, ",")
	if len(parts) < 4 {
		return s
	}
	if v, ok := parseF(parts[0]); ok {
		s.Util = &v
	}
	used, okU := parseF(parts[1])
	total, okT := parseF(parts[2])
	if okU && okT && total > 0 {
		v := used / total * 100
		s.Mem = &v
	}
	if v, ok := parseF(parts[3]); ok {
		s.Temp = &v
	}
	return s
}

func parseF(s string) (float64, bool) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
