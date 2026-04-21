package llm

import "sync"

// Usage holds token counts from a single LLM API call.
type Usage struct {
	InputTokens  int
	OutputTokens int
}

// UsageTracker accumulates token usage across multiple LLM calls for one user.
// It is safe for concurrent use.
type UsageTracker struct {
	mu           sync.Mutex
	inputTokens  int64
	outputTokens int64
	calls        int
}

// Add accumulates usage from a single call. No-op if u is nil.
func (t *UsageTracker) Add(u *Usage) {
	if u == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.inputTokens += int64(u.InputTokens)
	t.outputTokens += int64(u.OutputTokens)
	t.calls++
}

// Snapshot returns a point-in-time copy of the accumulated totals.
func (t *UsageTracker) Snapshot() UsageSnapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	return UsageSnapshot{
		InputTokens:  t.inputTokens,
		OutputTokens: t.outputTokens,
		Calls:        t.calls,
	}
}

// UsageSnapshot is a read-only copy of tracker state.
type UsageSnapshot struct {
	InputTokens  int64
	OutputTokens int64
	Calls        int
}

// UserUsageStore holds one UsageTracker per user ID.
// It creates trackers on first access and is safe for concurrent use.
type UserUsageStore struct {
	mu       sync.Mutex
	trackers map[string]*UsageTracker
}

// NewUserUsageStore creates an empty store.
func NewUserUsageStore() *UserUsageStore {
	return &UserUsageStore{trackers: make(map[string]*UsageTracker)}
}

// For returns the tracker for userID, creating one if it doesn't exist.
func (s *UserUsageStore) For(userID string) *UsageTracker {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.trackers[userID]; ok {
		return t
	}
	t := &UsageTracker{}
	s.trackers[userID] = t
	return t
}
