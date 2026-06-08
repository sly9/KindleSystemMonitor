package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// cpuTempNames is the priority-ordered list of LibreHardwareMonitor sensor
// names that name "the right" CPU package temperature. Mirrors the Python
// version's _CPU_TEMP_NAMES so behavior is identical across CPU vendors.
var cpuTempNames = []string{
	"Core (Tctl/Tdie)", // AMD Ryzen modern (Zen 4 / Zen 5)
	"CPU Package",      // Intel
	"Core (Tctl)",      // older AMD
	"Core Max",         // last-resort fallback
}

// lhmNode mirrors the node structure of LHM's /data.json tree.
type lhmNode struct {
	Text     string    `json:"Text"`
	Value    string    `json:"Value"`
	Children []lhmNode `json:"Children"`
}

// lhmReader fetches CPU temp from LHM's web server with a small TTL cache —
// the JSON is ~100KB so doing it every tick is wasteful.
type lhmReader struct {
	url string
	ttl time.Duration

	mu       sync.Mutex
	cachedAt time.Time
	cpuTemp  *float64
}

func (r *lhmReader) cpuTempC() *float64 {
	if r == nil || r.url == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.cachedAt.IsZero() && time.Since(r.cachedAt) < r.ttl {
		return r.cpuTemp
	}
	r.cpuTemp = fetchCPUTempLHM(r.url, 3*time.Second)
	r.cachedAt = time.Now()
	return r.cpuTemp
}

func fetchCPUTempLHM(url string, timeout time.Duration) *float64 {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var root lhmNode
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return nil
	}
	matches := map[string]float64{}
	walkLHM(root, matches)
	for _, name := range cpuTempNames {
		if v, ok := matches[name]; ok {
			return &v
		}
	}
	return nil
}

func walkLHM(n lhmNode, matches map[string]float64) {
	if strings.Contains(n.Value, "°C") {
		for _, name := range cpuTempNames {
			if n.Text == name {
				if v, ok := parseTempCelsius(n.Value); ok {
					if _, exists := matches[name]; !exists {
						matches[name] = v
					}
				}
				break
			}
		}
	}
	for _, c := range n.Children {
		walkLHM(c, matches)
	}
}

func parseTempCelsius(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "°C")
	s = strings.TrimSpace(s)
	// Some locales render decimals with comma. Normalize.
	s = strings.ReplaceAll(s, ",", ".")
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
