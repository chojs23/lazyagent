package tui

import (
	"sync"
	"testing"
)

func TestDebugOverlayConcurrentAccess(t *testing.T) {
	var overlay debugOverlay
	overlay.toggle()

	var wg sync.WaitGroup
	for worker := range 4 {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for step := range 200 {
				overlay.add("worker=%d step=%d", worker, step)
				overlay.scrollUp(1)
				overlay.scrollDown(1)
				_ = overlay.view(80, 20)
			}
		}(worker)
	}

	wg.Wait()

	snapshot := overlay.snapshot()
	if !snapshot.visible {
		t.Fatal("overlay should stay visible")
	}
	if len(snapshot.entries) == 0 {
		t.Fatal("overlay should keep logged entries")
	}
	if got := len(snapshot.entries); got > debugLogCapacity {
		t.Fatalf("entry count = %d, want <= %d", got, debugLogCapacity)
	}
}
