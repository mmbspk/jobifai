package bot

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/user/jobifai/internal/domain"
)

type formFieldFlight struct {
	done     chan struct{}
	proposal *LLMFieldProposal
	err      error
}

// FormLearningCoordinator shares only concurrently requested proposals. It never
// activates a mapping or keeps a candidate answer for a subsequent warm run.
// Overlapping batches reserve each missing field separately and batch only the
// fields owned by this caller; waiters translate the result to their fresh refs.
type FormLearningCoordinator struct {
	mu      sync.Mutex
	flights map[string]*formFieldFlight
}

type formInterpretFunc func(context.Context, *domain.ResumeProfile, StepSnapshot, []string) ([]LLMFieldProposal, error)

func (c *FormLearningCoordinator) Interpret(ctx context.Context, scope string, profile *domain.ResumeProfile, snap StepSnapshot, refs []string, interpret formInterpretFunc) ([]LLMFieldProposal, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		return nil, err
	}
	profileHash := sha256.Sum256(raw)
	prefix := fmt.Sprintf("%s\x00%s\x00%s\x00%x\x00", scope, snap.Platform, snap.EmployerKey, profileHash)
	type request struct {
		ref, key string
		flight   *formFieldFlight
		owned    bool
	}
	requests := make([]request, 0, len(refs))
	var ownRefs []string
	c.mu.Lock()
	if c.flights == nil {
		c.flights = map[string]*formFieldFlight{}
	}
	for _, ref := range refs {
		field, ok := snap.FieldByRef(ref)
		if !ok {
			continue
		}
		key := prefix + field.FieldSignature(snap.Platform)
		flight, exists := c.flights[key]
		if !exists {
			flight = &formFieldFlight{done: make(chan struct{})}
			c.flights[key] = flight
			ownRefs = append(ownRefs, ref)
		}
		requests = append(requests, request{ref: ref, key: key, flight: flight, owned: !exists})
	}
	c.mu.Unlock()
	if len(ownRefs) > 0 {
		proposals, callErr := interpret(ctx, profile, snap, ownRefs)
		byRef := map[string]*LLMFieldProposal{}
		duplicates := map[string]bool{}
		for _, proposal := range proposals {
			if byRef[proposal.Ref] != nil {
				duplicates[proposal.Ref] = true
			}
			if proposal.grounded == nil {
				proposal.Value = "" // unverified answers are never shared
			}
			copy := proposal
			byRef[proposal.Ref] = &copy
		}
		c.mu.Lock()
		for _, request := range requests {
			if !request.owned {
				continue
			}
			request.flight.proposal, request.flight.err = byRef[request.ref], callErr
			if duplicates[request.ref] {
				request.flight.proposal = nil
			}
			delete(c.flights, request.key)
			close(request.flight.done)
		}
		c.mu.Unlock()
	}
	var out []LLMFieldProposal
	for _, request := range requests {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-request.flight.done:
		}
		if request.flight.err != nil {
			return nil, request.flight.err
		}
		if request.flight.proposal != nil {
			proposal := *request.flight.proposal
			proposal.Ref = request.ref
			out = append(out, proposal)
		}
	}
	return out, nil
}
