package metrics

import "time"

type Metrics struct {
	CPU     *float64 `json:"cpu,omitempty"`
	Mem     *float64 `json:"mem,omitempty"`
	GPU     *float64 `json:"gpu,omitempty"`
	VRAM    *float64 `json:"vram,omitempty"`
	CPUTemp *float64 `json:"cpu_temp,omitempty"`
	GPUTemp *float64 `json:"gpu_temp,omitempty"`
}

type Provider interface {
	Read() Metrics
}

// Options configures optional data sources. Zero value works (all defaults to N/A).
type Options struct {
	// LHMUrl, if non-empty, is the LibreHardwareMonitor web-server endpoint
	// (e.g. "http://localhost:8085/data.json") used to read CPU package temp.
	// Windows has no easy native API for this, so users who want CPU temp
	// run LHM and point us at it.
	LHMUrl string

	// LHMCacheTTL throttles LHM fetches. Zero → 5 seconds.
	LHMCacheTTL time.Duration
}

type gpuSample struct {
	Util *float64
	Mem  *float64
	Temp *float64
}

type defaultProvider struct {
	pawn *pawnReader
	lhm  *lhmReader
}

// NewDefaultProvider returns the production provider.
//
// CPU temperature resolution order:
//  1. PawnIO via embedded AMDFamily17.bin (Windows + admin + PawnIO installed)
//  2. LibreHardwareMonitor HTTP if opts.LHMUrl is set (fallback)
//  3. N/A
func NewDefaultProvider(opts Options) Provider {
	p := &defaultProvider{
		pawn: newPawnReader(),
	}
	if opts.LHMUrl != "" {
		ttl := opts.LHMCacheTTL
		if ttl <= 0 {
			ttl = 5 * time.Second
		}
		p.lhm = &lhmReader{url: opts.LHMUrl, ttl: ttl}
	}
	return p
}

func (p *defaultProvider) Read() Metrics {
	var m Metrics
	cpu, mem := readCPUMem()
	m.CPU = cpu
	m.Mem = mem
	g := readGPU()
	m.GPU = g.Util
	m.VRAM = g.Mem
	m.GPUTemp = g.Temp
	if p.pawn != nil {
		m.CPUTemp = p.pawn.CPUTempC()
	}
	if m.CPUTemp == nil && p.lhm != nil {
		m.CPUTemp = p.lhm.cpuTempC()
	}
	return m
}

// DoctorPawnIO is a one-shot status probe exposed to the `doctor` command.
// On non-Windows platforms this always reports unsupported.
func DoctorPawnIO() (ready bool, detail string, tempC *float64) {
	p := newPawnReader()
	if p == nil {
		return false, "PawnIO is Windows-only", nil
	}
	ready, detail = p.Status()
	if ready {
		tempC = p.CPUTempC()
	}
	return ready, detail, tempC
}
