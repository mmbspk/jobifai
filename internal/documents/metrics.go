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
	// ArtifactServeHits counts calls to PDFBytes that served a PDF blob straight
	// from artifact storage without reconstruction.
	ArtifactServeHits atomic.Int64
	// ArtifactServeBytesTotal is the cumulative byte count of all artifact-served responses.
	ArtifactServeBytesTotal atomic.Int64
	// Reconstructions counts calls to PDFBytes that triggered on-demand
	// reconstruction because the artifact blob was evicted or never persisted.
	Reconstructions atomic.Int64
	// ReconstructionBytesTotal is the cumulative byte count of all reconstructed
	// responses.
	ReconstructionBytesTotal atomic.Int64
}

// DocumentsMetricsSnapshot is a point-in-time snapshot returned by the admin
// metrics endpoint.  In addition to the in-process counters it carries the
// current retained-artifact gauge queried from the database.
type DocumentsMetricsSnapshot struct {
	// In-process PDF-serve counters (reset on restart).
	ArtifactServeHits        int64 `json:"artifact_serve_hits"`
	ArtifactServeBytesTotal  int64 `json:"artifact_serve_bytes_total"`
	Reconstructions          int64 `json:"reconstructions"`
	ReconstructionBytesTotal int64 `json:"reconstruction_bytes_total"`
	// In-process LLM content cache-hit counter (reset on restart).
	// Populated from llmreuse.Store.CacheHits by the admin handler.
	LLMCacheHits int64 `json:"llm_cache_hits"`
	// Current retained-artifact gauge (queried from DB at request time).
	RetainedArtifacts      int64 `json:"retained_artifacts"`
	RetainedArtifactBytes  int64 `json:"retained_artifact_bytes"`
}

// Snapshot returns an atomic snapshot of the in-process counter values.
// The caller is responsible for populating LLMCacheHits and the retained-
// artifact fields from external sources before returning the snapshot.
func (m *ServiceMetrics) Snapshot() DocumentsMetricsSnapshot {
	return DocumentsMetricsSnapshot{
		ArtifactServeHits:        m.ArtifactServeHits.Load(),
		ArtifactServeBytesTotal:  m.ArtifactServeBytesTotal.Load(),
		Reconstructions:          m.Reconstructions.Load(),
		ReconstructionBytesTotal: m.ReconstructionBytesTotal.Load(),
	}
}
