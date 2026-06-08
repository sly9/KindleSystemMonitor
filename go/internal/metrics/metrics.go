package metrics

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

type gpuSample struct {
	Util *float64
	Mem  *float64
	Temp *float64
}

type defaultProvider struct{}

func NewDefaultProvider() Provider { return defaultProvider{} }

func (defaultProvider) Read() Metrics {
	var m Metrics
	cpu, mem := readCPUMem()
	m.CPU = cpu
	m.Mem = mem
	g := readGPU()
	m.GPU = g.Util
	m.VRAM = g.Mem
	m.GPUTemp = g.Temp
	return m
}
