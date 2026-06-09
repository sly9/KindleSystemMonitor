//go:build darwin

package metrics

import (
	"strings"

	"github.com/shirou/gopsutil/v4/sensors"
)

// readNativeTemps reads CPU and GPU temperatures from Apple's HID temperature
// sensors via gopsutil (uses IOKit/IOHIDEventSystem, no sudo needed).
//
// Apple Silicon sensor key patterns observed in the wild:
//   - "PMU tdie*"  — CPU die temperatures (one per cluster or core group)
//   - "PMU TP*g"   — GPU cluster temperatures ("g" suffix = GPU)
//   - "PMU tdev*"  — device/board temperatures (ignored here)
//   - "PMU tcal"   — calibration reference (ignored)
//
// We report the max across the relevant sensors for each domain.
func readNativeTemps() (cpuTemp, gpuTemp *float64) {
	all, err := sensors.SensorsTemperatures()
	if err != nil || len(all) == 0 {
		return nil, nil
	}
	var maxCPU, maxGPU float64
	for _, t := range all {
		v := t.Temperature
		if v < 20 || v > 120 {
			continue
		}
		key := t.SensorKey
		keyLow := strings.ToLower(key)

		// Generic match for any platform / future sensor name changes.
		isCPU := strings.Contains(keyLow, "cpu")
		isGPU := strings.Contains(keyLow, "gpu")

		// Apple Silicon: "PMU tdie*" = CPU die, "PMU TP*g" = GPU cluster.
		if strings.HasPrefix(key, "PMU tdie") {
			isCPU = true
		} else if len(key) >= 8 && strings.HasPrefix(key, "PMU TP") && strings.HasSuffix(key, "g") {
			isGPU = true
		}

		if isCPU && v > maxCPU {
			maxCPU = v
		}
		if isGPU && v > maxGPU {
			maxGPU = v
		}
	}
	if maxCPU > 0 {
		cpuTemp = &maxCPU
	}
	if maxGPU > 0 {
		gpuTemp = &maxGPU
	}
	return
}
