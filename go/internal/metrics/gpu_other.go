//go:build !windows

package metrics

// On non-Windows we don't ship a GPU reader yet. macOS Apple Silicon has no
// NVIDIA path; powermetrics is the typical route but needs sudo (Plan §6).
func readGPU() gpuSample {
	return gpuSample{}
}
