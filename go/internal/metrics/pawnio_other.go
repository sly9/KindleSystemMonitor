//go:build !windows

package metrics

// PawnIO is Windows-only. On other platforms newPawnReader returns nil and
// the provider falls back to whatever the LHM HTTP path provides (also nil
// in practice — macOS has no LHM equivalent).
type pawnReader struct{}

func newPawnReader() *pawnReader            { return nil }
func (*pawnReader) CPUTempC() *float64      { return nil }
func (*pawnReader) Status() (bool, string)  { return false, "PawnIO is Windows-only" }
