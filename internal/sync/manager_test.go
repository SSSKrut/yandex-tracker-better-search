package sync

import (
	"sync"
	"testing"
	"time"
)

func TestManagerStateConcurrentAccess(t *testing.T) {
	m := &Manager{}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.updateState(func(state *SyncState) {
				state.LastUpdatedAt = time.Now()
			})
		}()
		go func() {
			defer wg.Done()
			_ = m.getState()
		}()
	}
	wg.Wait()
}
