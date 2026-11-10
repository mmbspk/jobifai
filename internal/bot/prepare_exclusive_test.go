package bot

import "testing"

// TestTryBeginPrepare_InProcessExclusivity verifies the in-process prepare guard that
// prevents overlapping browser work for the same user+job. This is the invariant the DB
// stale-reclaim cannot provide alone: a hung-but-alive run must block a concurrent run even
// after ten minutes elapse, while a crashed run (empty map after restart) must not.
func TestTryBeginPrepare_InProcessExclusivity(t *testing.T) {
	m := &Manager{}

	if !m.TryBeginPrepare("u", "j", "p1") {
		t.Fatal("first prepare must acquire ownership")
	}
	if m.TryBeginPrepare("u", "j", "p2") {
		t.Fatal("second prepare for same user+job must be rejected while p1 holds ownership")
	}

	// A superseded run's EndPrepare must not release the active holder's claim.
	m.EndPrepare("u", "j", "p2")
	if m.TryBeginPrepare("u", "j", "p3") {
		t.Fatal("ownership must still be held by p1 after a mismatched EndPrepare")
	}

	// A different job is independent.
	if !m.TryBeginPrepare("u", "j2", "px") {
		t.Fatal("a different job must acquire ownership independently")
	}

	// The real holder releases; a new run may then acquire.
	m.EndPrepare("u", "j", "p1")
	if !m.TryBeginPrepare("u", "j", "p4") {
		t.Fatal("ownership must be acquirable after the holder releases")
	}
}
