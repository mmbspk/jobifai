package retention

import (
	"context"
	"sync"
	"time"
)

const coordinatorPollInterval = 10 * time.Millisecond

// ActivityCoordinator serializes eviction with user document/application work.
type ActivityCoordinator struct {
	users sync.Map // userID -> *userActivity
}

type userActivity struct {
	mu         sync.Mutex
	activeWork int
	evicting   bool
}

func (c *ActivityCoordinator) userState(userID string) *userActivity {
	if c == nil {
		return nil
	}
	v, _ := c.users.LoadOrStore(userID, &userActivity{})
	return v.(*userActivity)
}

func waitWhileLocked(ctx context.Context, st *userActivity, pred func() bool) error {
	for pred() {
		if err := ctx.Err(); err != nil {
			return err
		}
		st.mu.Unlock()
		select {
		case <-ctx.Done():
			st.mu.Lock()
			return ctx.Err()
		case <-time.After(coordinatorPollInterval):
		}
		st.mu.Lock()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// BeginWork marks in-flight work and blocks while eviction runs.
func (c *ActivityCoordinator) BeginWork(ctx context.Context, userID string) error {
	st := c.userState(userID)
	if st == nil {
		return nil
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if err := waitWhileLocked(ctx, st, func() bool { return st.evicting }); err != nil {
		return err
	}
	st.activeWork++
	return nil
}

// EndWork clears a work marker started with BeginWork.
func (c *ActivityCoordinator) EndWork(userID string) {
	st := c.userState(userID)
	if st == nil {
		return
	}
	st.mu.Lock()
	if st.activeWork > 0 {
		st.activeWork--
	}
	st.mu.Unlock()
}

// WithEviction runs fn while eviction is exclusive; new work cannot start until fn returns.
func (c *ActivityCoordinator) WithEviction(ctx context.Context, userID string, fn func() error) error {
	st := c.userState(userID)
	if st == nil {
		return fn()
	}
	st.mu.Lock()
	if err := waitWhileLocked(ctx, st, func() bool { return st.activeWork > 0 || st.evicting }); err != nil {
		st.mu.Unlock()
		return err
	}
	st.evicting = true
	st.mu.Unlock()
	defer func() {
		st.mu.Lock()
		st.evicting = false
		st.mu.Unlock()
	}()
	return fn()
}

// WithWork runs fn under an active-work marker (blocks eviction).
func (c *ActivityCoordinator) WithWork(ctx context.Context, userID string, fn func() error) error {
	if err := c.BeginWork(ctx, userID); err != nil {
		return err
	}
	defer c.EndWork(userID)
	return fn()
}
