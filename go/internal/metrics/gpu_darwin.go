//go:build darwin

package metrics

import (
	"os/exec"
	"regexp"
	"strconv"
)

// Apple Silicon exposes GPU stats via the IOAccelerator registry entry.
// "Device Utilization %" is the overall GPU busy percentage (0-100).
// No sudo needed; ioreg is a standard macOS CLI tool.
var ioregUtilRe = regexp.MustCompile(`"Device Utilization %"\s*=\s*(\d+)`)

func readGPU() gpuSample {
	out, err := exec.Command("ioreg", "-r", "-c", "IOAccelerator").Output()
	if err != nil {
		return gpuSample{}
	}
	var s gpuSample
	if m := ioregUtilRe.FindSubmatch(out); m != nil {
		if v, err := strconv.ParseFloat(string(m[1]), 64); err == nil {
			s.Util = &v
		}
	}
	return s
}
