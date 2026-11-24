package budget

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewAndClamp(t *testing.T) {
	p := New(0)
	if p.Capacity() != 1 {
		t.Fatalf("expected capacity 1 for <=0 input, got %d", p.Capacity())
	}
	if p.InUse() != 0 {
		t.Fatalf("expected initial InUse 0, got %d", p.InUse())
	}

	p2 := New(3)
	if p2.Capacity() != 3 {
		t.Fatalf("expected capacity 3, got %d", p2.Capacity())
	}
	// Weight <= 0 clamps to 1, weight > capacity clamps to capacity.
	if !p2.TryAcquire(0) {
		t.Fatalf("expected TryAcquire(0) to succeed")
	}
	if p2.InUse() != 1 {
		t.Fatalf("expected InUse 1 after TryAcquire(0), got %d", p2.InUse())
	}
	p2.Release(0)
	if p2.InUse() != 0 {
		t.Fatalf("expected InUse 0 after Release(0), got %d", p2.InUse())
	}

	if !p2.TryAcquire(99) {
		t.Fatalf("expected TryAcquire(99) clamped to capacity to succeed")
	}
	if p2.InUse() != 3 {
		t.Fatalf("expected InUse 3 after clamped acquire, got %d", p2.InUse())
	}
	if p2.TryAcquire(1) {
		t.Fatalf("expected TryAcquire(1) to fail when pool is full")
	}
	p2.Release(99)
	if p2.InUse() != 0 {
		t.Fatalf("expected InUse 0 after Release(99), got %d", p2.InUse())
	}
}

func TestDefaultAndEnvOverride(t *testing.T) {
	t.Setenv(envBudgetKey, "7")
	p := Default()
	if p.Capacity() != 7 {
		t.Fatalf("expected env override capacity 7, got %d", p.Capacity())
	}

	t.Setenv(envBudgetKey, "invalid")
	pInvalid := Default()
	want := int64(runtime.NumCPU() - 1)
	if want < 1 {
		want = 1
	}
	if pInvalid.Capacity() != want {
		t.Fatalf("expected fallback capacity %d, got %d", want, pInvalid.Capacity())
	}

	t.Setenv(envBudgetKey, "-5")
	pNeg := Default()
	if pNeg.Capacity() != want {
		t.Fatalf("expected fallback capacity %d for negative env, got %d", want, pNeg.Capacity())
	}

	t.Setenv(envBudgetKey, "")
	pEmpty := Default()
	if pEmpty.Capacity() != want {
		t.Fatalf("expected fallback capacity %d for empty env, got %d", want, pEmpty.Capacity())
	}
}

func TestGlobalSingleton(t *testing.T) {
	g1 := Global()
	g2 := Global()
	if g1 == nil || g1 != g2 {
		t.Fatalf("expected Global() to return a non-nil singleton")
	}
}

func TestAcquireCancelAndDo(t *testing.T) {
	p := New(1)
	if err := p.Acquire(context.Background(), 1); err != nil {
		t.Fatalf("initial Acquire failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := p.Acquire(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if err := p.Do(ctx, 1, func(context.Context) error { return nil }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected Do to return DeadlineExceeded, got %v", err)
	}

	p.Release(1)

	if err := p.Do(context.Background(), 1, nil); err != nil {
		t.Fatalf("expected nil error for nil fn, got %v", err)
	}
	if p.InUse() != 0 {
		t.Fatalf("expected InUse 0 after Do(nil), got %d", p.InUse())
	}

	sentinel := errors.New("boom")
	if err := p.Do(context.Background(), 1, func(context.Context) error {
		if p.InUse() != 1 {
			t.Errorf("expected InUse 1 inside Do, got %d", p.InUse())
		}
		return sentinel
	}); !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error, got %v", err)
	}
	if p.InUse() != 0 {
		t.Fatalf("expected InUse 0 after Do error, got %d", p.InUse())
	}
}

func TestConcurrencyBound(t *testing.T) {
	const limit = 2
	p := New(limit)

	var active atomic.Int32
	var maxObserved atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := p.Do(context.Background(), 1, func(context.Context) error {
				cur := active.Add(1)
				for {
					prev := maxObserved.Load()
					if cur <= prev || maxObserved.CompareAndSwap(prev, cur) {
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
				active.Add(-1)
				return nil
			})
			if err != nil {
				t.Errorf("unexpected Do error: %v", err)
			}
		}()
	}
	wg.Wait()

	if maxObserved.Load() > limit {
		t.Fatalf("concurrency exceeded limit %d: observed %d", limit, maxObserved.Load())
	}
	if p.InUse() != 0 {
		t.Fatalf("expected InUse 0 after all goroutines finish, got %d", p.InUse())
	}
}
