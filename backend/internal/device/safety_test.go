package device

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeSwitch records SetOutput calls and can fail a number of initial calls.
type fakeSwitch struct {
	mu           sync.Mutex
	calls        int
	failFirst    int
	lastCtxAlive bool
	err          error
}

func (f *fakeSwitch) SetOutput(ctx context.Context, on bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastCtxAlive = ctx.Err() == nil
	if f.calls <= f.failFirst {
		if f.err != nil {
			return f.err
		}
		return errors.New("boom")
	}
	return nil
}

func (f *fakeSwitch) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func TestSafeOutputOffUsesFreshContext(t *testing.T) {
	sw := &fakeSwitch{}
	if err := SafeOutputOff(sw, nil); err != nil {
		t.Fatalf("SafeOutputOff: unexpected error %v", err)
	}
	if !sw.lastCtxAlive {
		t.Fatal("SafeOutputOff must call SetOutput on a live (non-cancelled) context")
	}
	if got := sw.callCount(); got != 1 {
		t.Fatalf("SetOutput calls = %d, want 1 on immediate success", got)
	}
}

func TestSafeOutputOffRetriesThenSucceeds(t *testing.T) {
	restore := safeOutputOffBackoff
	safeOutputOffBackoff = 1 // keep the test fast
	defer func() { safeOutputOffBackoff = restore }()

	sw := &fakeSwitch{failFirst: safeOutputOffTries - 1}
	if err := SafeOutputOff(sw, nil); err != nil {
		t.Fatalf("SafeOutputOff should succeed on the last retry, got %v", err)
	}
	if got := sw.callCount(); got != safeOutputOffTries {
		t.Fatalf("SetOutput calls = %d, want %d", got, safeOutputOffTries)
	}
}

func TestSafeOutputOffReturnsFinalError(t *testing.T) {
	restore := safeOutputOffBackoff
	safeOutputOffBackoff = 1
	defer func() { safeOutputOffBackoff = restore }()

	want := errors.New("offline")
	sw := &fakeSwitch{failFirst: 1 << 30, err: want}
	if err := SafeOutputOff(sw, nil); !errors.Is(err, want) {
		t.Fatalf("SafeOutputOff error = %v, want %v", err, want)
	}
	if got := sw.callCount(); got != safeOutputOffTries {
		t.Fatalf("SetOutput calls = %d, want %d exhausted retries", got, safeOutputOffTries)
	}
}

func TestInterlockSingleOwner(t *testing.T) {
	var il Interlock
	if !il.Acquire("sequence") {
		t.Fatal("first Acquire must succeed on a free interlock")
	}
	if il.Acquire("charge") {
		t.Fatal("second engine must not Acquire while owned")
	}
	if il.Acquire("sequence") {
		t.Fatal("same engine must not re-Acquire while owned")
	}
	if !il.Busy() || il.Owner() != "sequence" {
		t.Fatalf("Owner=%q Busy=%v, want sequence/true", il.Owner(), il.Busy())
	}
	// A mismatched release is a no-op.
	il.Release("charge")
	if il.Owner() != "sequence" {
		t.Fatal("mismatched Release must not free the interlock")
	}
	il.Release("sequence")
	if il.Busy() {
		t.Fatal("Release by the owner must free the interlock")
	}
	if !il.Acquire("charge") {
		t.Fatal("Acquire must succeed once freed")
	}
}

func TestInterlockConcurrentAcquireExactlyOne(t *testing.T) {
	var il Interlock
	const n = 64
	var wins int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if il.Acquire("sequence") {
				atomic.AddInt64(&wins, 1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if wins != 1 {
		t.Fatalf("concurrent Acquire winners = %d, want exactly 1", wins)
	}
}
