package retention

import "sync"

// ActivityCoordinator serializes eviction with user document/application work.
type ActivityCoordinator struct {
	users sync.Map // userID -> *userActivity
}

type userActivity struct {
	mu          sync.RWMutex
	activeWork  int
	evicting    bool
}

func (c *ActivityCoordinator) userState(userID string) *userActivity {
	if c == nil {
		return nil
	}
	v, _ := c.users.LoadOrStore(userID, &userActivity{})
	return v.(*userActivity)
}

// BeginWork marks upload/download/prepare/submit in flight for userID.
func (c *ActivityCoordinator) BeginWork(userID string) {
	st := c.userState(userID)
	if st == nil {
		return
	}
	st.mu.Lock()
	st.activeWork++
	st.mu.Unlock()
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

// WithEviction runs fn while holding the eviction lock and blocking new work.
func (c *ActivityCoordinator) WithEviction(userID string, fn func() error) error {
	st := c.userState(userID)
	if st == nil {
		return fn()
	}
	st.mu.Lock()
	for st.activeWork > 0 {
		st.mu.Unlock()
		st.mu.Lock()
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

// EvictionBlocked reports whether eviction is unsafe because work is active.
func (c *ActivityCoordinator) EvictionBlocked(userID string) bool {
	st := c.userState(userID)
	if st == nil {
		return false
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.activeWork > 0 || st.evicting
}
