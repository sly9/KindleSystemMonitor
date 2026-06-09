//go:build !darwin

package metrics

func readNativeTemps() (cpuTemp, gpuTemp *float64) { return nil, nil }
