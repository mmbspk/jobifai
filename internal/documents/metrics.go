package documents

import "sync/atomic"

// ServiceMetrics accumulates document-serving counters for the lifetime of the
// process.  Values are reset to zero on restart, making them appropriate for
// operational dashboards but not for billing or auditing.
//
// Attach an instance to Service.Metrics before the server starts accepting
// requests.  All counters are nil-safe: if Service.Metrics is nil, PDFBytes
// silently skips the increments.
type ServiceMetrics struct {
	// ReuseHits counts calls to PDFBytes that served a blob straight from
	// artifact storage (no reconstruction required).
	ReuseHits atomic.Int64
	// ReuseBytesTotal is the cumulative byte count of all reuse-hit responses.
	ReuseBytesTotal atomic.Int64
	// Reconstructions counts calls to PDFBytes that triggered on-demand
	// reconstruction because the artifact blob was evicted or never persisted.
	Reconstructions atomic.Int64
	// ReconstructionBytesTotal is the cumulative byte count of all reconstructed
	// responses.
	ReconstructionBytesTotal atomic.Int64
}

// DocumentsMetricsSnapshot is a point-in-time snapshot of ServiceMetrics.
type DocumentsMetricsSnapshot struct {
	ReuseHits                int64 `json:"reuse_hits"`
	ReuseBytesTotal          int64 `json:"reuse_bytes_total"`
	Reconstructions          int64 `json:"reconstructions"`
	ReconstructionBytesTotal int64 `json:"reconstruction_bytes_total"`
}

// Snapshot returns an atomic snapshot of the current counter values.
func (m *ServiceMetrics) Snapshot() DocumentsMetricsSnapshot {
	return DocumentsMetricsSnapshot{
		ReuseHits:                m.ReuseHits.Load(),
		ReuseBytesTotal:          m.ReuseBytesTotal.Load(),
		Reconstructions:          m.Reconstructions.Load(),
		ReconstructionBytesTotal: m.ReconstructionBytesTotal.Load(),
	}
}
