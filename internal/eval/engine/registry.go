package engine

import (
	"context"
	"sync"
)

// RunRegistry tracks in-flight eval runs for cancellation.
type RunRegistry struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

var Runs RunRegistry

func init() {
	Runs.cancels = map[string]context.CancelFunc{}
}

func (r *RunRegistry) Register(id string, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[id] = cancel
}

func (r *RunRegistry) Unregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, id)
}

func (r *RunRegistry) Cancel(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cancels[id]; ok {
		c()
		return true
	}
	return false
}
