package engine

import (
	"sync"

	"github.com/user/jobifai/internal/eval/candidate"
	"github.com/user/jobifai/internal/eval/preflight"
)

// Candidate lifecycle states persisted in run summary_json.candidates.
const (
	CandidateRunnable          = preflight.StateRunnable
	CandidateCompleted         = "completed"
	CandidateFailed            = "failed"
	CandidateUnavailableModel  = preflight.StateUnavailableModel
	CandidateMissingCredential = preflight.StateMissingCredential
	CandidateCapabilityMismatch = preflight.StateCapabilityMismatch
	CandidatePricingUnresolved = preflight.StatePricingUnresolved
)

// CandidateRecord is machine-readable eval candidate status for a run.
type CandidateRecord struct {
	ID                string `json:"id"`
	Provider          string `json:"provider"`
	Model             string `json:"model"`
	Effort            string `json:"effort,omitempty"`
	Status            string `json:"status"`
	Reason            string `json:"reason,omitempty"`
	ProviderErrorCode string `json:"provider_error_code,omitempty"`
	ExpectedCases     int    `json:"expected_cases"`
	CompletedCases    int    `json:"completed_cases"`
}

type candidateRegistry struct {
	mu      sync.Mutex
	records map[string]*CandidateRecord
}

func newCandidateRegistry(specs []candidate.Spec, expectedCases int) *candidateRegistry {
	r := &candidateRegistry{records: map[string]*CandidateRecord{}}
	for _, s := range specs {
		id := s.ID()
		r.records[id] = &CandidateRecord{
			ID: id, Provider: s.Provider, Model: s.Model, Effort: s.Effort,
			Status: CandidateRunnable, ExpectedCases: expectedCases,
		}
	}
	return r
}

func (r *candidateRegistry) setStatus(id, status, reason, code string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[id]
	if !ok {
		return
	}
	rec.Status = status
	rec.Reason = reason
	rec.ProviderErrorCode = code
	if status != CandidateRunnable && status != CandidateCompleted {
		rec.ExpectedCases = rec.CompletedCases
	}
}

func (r *candidateRegistry) addCompleted(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.records[id]; ok {
		rec.CompletedCases++
	}
}

func (r *candidateRegistry) finalizeRunnable(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[id]
	if !ok || rec.Status != CandidateRunnable {
		return
	}
	if rec.CompletedCases >= rec.ExpectedCases && rec.ExpectedCases > 0 {
		rec.Status = CandidateCompleted
	} else if rec.CompletedCases > 0 {
		rec.Status = CandidateFailed
		rec.Reason = "incomplete case coverage"
	}
}

func (r *candidateRegistry) snapshot() []CandidateRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CandidateRecord, 0, len(r.records))
	for _, rec := range r.records {
		cp := *rec
		out = append(out, cp)
	}
	return out
}

// modelAvailability tracks provider-level model unavailability for one run.
type modelAvailability struct {
	mu          sync.Mutex
	unavailable map[string]preflight.Status
}

func newModelAvailability() *modelAvailability {
	return &modelAvailability{unavailable: map[string]preflight.Status{}}
}

func modelKey(provider, model string) string {
	return provider + "\x00" + model
}

func (m *modelAvailability) get(provider, model string) (preflight.Status, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.unavailable[modelKey(provider, model)]
	return st, ok
}

func (m *modelAvailability) set(provider, model string, st preflight.Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unavailable[modelKey(provider, model)] = st
}

type runBudget struct {
	mu           sync.Mutex
	spentRaw     int64
	spentBudget  int64
	reserved     int64
	stop         bool
	maxMicro     int64
}

func newRunBudget(maxMicro int64) *runBudget {
	return &runBudget{maxMicro: maxMicro}
}

func (b *runBudget) shouldStop() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stop
}

func (b *runBudget) requestStop() {
	b.mu.Lock()
	b.stop = true
	b.mu.Unlock()
}

func (b *runBudget) overBudget() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxMicro > 0 && b.spentBudget+b.reserved >= b.maxMicro
}

func (b *runBudget) tryReserve(micro int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.stop {
		return false
	}
	if micro <= 0 {
		micro = defaultPerCallReserveMicro
	}
	if b.maxMicro > 0 && b.spentBudget+b.reserved+micro > b.maxMicro {
		b.stop = true
		return false
	}
	b.reserved += micro
	return true
}

func (b *runBudget) reconcile(reserved, rawMicro, budgetMicro int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if reserved > b.reserved {
		reserved = b.reserved
	}
	b.reserved -= reserved
	if rawMicro > 0 {
		b.spentRaw += rawMicro
	}
	if budgetMicro > 0 {
		b.spentBudget += budgetMicro
	} else if reserved > 0 && budgetMicro == 0 {
		// Failed call: still count reservation as spend so budget stays fail-closed.
		b.spentBudget += reserved
	}
	if b.maxMicro > 0 && b.spentBudget >= b.maxMicro {
		b.stop = true
	}
}

func (b *runBudget) charge(rawMicro, budgetMicro int64) (spentRaw, spentBudget int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if rawMicro > 0 {
		b.spentRaw += rawMicro
	}
	if budgetMicro > 0 {
		b.spentBudget += budgetMicro
	}
	if b.maxMicro > 0 && b.spentBudget >= b.maxMicro {
		b.stop = true
	}
	return b.spentRaw, b.spentBudget
}

func (b *runBudget) totals() (raw, budget int64, stop bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spentRaw, b.spentBudget + b.reserved, b.stop
}
