package engine

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunBudget_ConcurrentChargeAndStop(t *testing.T) {
	t.Parallel()
	b := newRunBudget(5000)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				if b.shouldStop() {
					return
				}
				b.charge(100, 200)
				if b.overBudget() {
					b.requestStop()
				}
			}
		}()
	}
	wg.Wait()
	raw, budget, stop := b.totals()
	require.True(t, stop || budget >= 5000)
	require.GreaterOrEqual(t, budget, int64(0))
	require.GreaterOrEqual(t, raw, int64(0))
}

func TestRunBudget_ConcurrentReserveDoesNotExceedCap(t *testing.T) {
	t.Parallel()
	const capMicro int64 = 50_000
	const reserveEach int64 = 15_000
	b := newRunBudget(capMicro)
	var wg sync.WaitGroup
	var reserved int32
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if b.tryReserve(reserveEach) {
				atomic.AddInt32(&reserved, 1)
			}
		}()
	}
	wg.Wait()
	_, budget, _ := b.totals()
	require.LessOrEqual(t, budget, capMicro)
	require.LessOrEqual(t, int64(reserved)*reserveEach, capMicro)
}
