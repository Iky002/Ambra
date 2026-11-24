package budget

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"
)

const envBudgetKey = "AMBRA_BG_BUDGET"

// Pool limits concurrent background CPU/IO heavy work (such as ffprobe,
// online metadata scraping, and thumbnail generation) across the process
// so foreground playback and API requests stay responsive.
type Pool struct {
	sem      *semaphore.Weighted
	capacity int64
	inUse    atomic.Int64
}

var (
	globalOnce sync.Once
	globalPool *Pool
)

// New creates a Pool with the given positive capacity.
// Capacities <= 0 are clamped to 1.
func New(capacity int64) *Pool {
	if capacity <= 0 {
		capacity = 1
	}
	return &Pool{
		sem:      semaphore.NewWeighted(capacity),
		capacity: capacity,
	}
}

// Default creates a new Pool using AMBRA_BG_BUDGET if it is a valid positive
// integer, otherwise max(1, runtime.NumCPU()-1).
func Default() *Pool {
	return New(defaultCapacity())
}

// Global returns the process-wide singleton Pool initialized via Default().
func Global() *Pool {
	globalOnce.Do(func() {
		globalPool = Default()
	})
	return globalPool
}

func defaultCapacity() int64 {
	if raw := strings.TrimSpace(os.Getenv(envBudgetKey)); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	n := int64(runtime.NumCPU() - 1)
	if n < 1 {
		n = 1
	}
	return n
}

func (p *Pool) clampWeight(weight int64) int64 {
	if weight <= 0 {
		return 1
	}
	if weight > p.capacity {
		return p.capacity
	}
	return weight
}

// Capacity returns the maximum total weight of the pool.
func (p *Pool) Capacity() int64 {
	return p.capacity
}

// InUse returns the weight currently acquired in the pool.
func (p *Pool) InUse() int64 {
	return p.inUse.Load()
}

// Acquire blocks until weight units are available or ctx is cancelled.
func (p *Pool) Acquire(ctx context.Context, weight int64) error {
	w := p.clampWeight(weight)
	if err := p.sem.Acquire(ctx, w); err != nil {
		return err
	}
	p.inUse.Add(w)
	return nil
}

// TryAcquire attempts to acquire weight units without blocking.
func (p *Pool) TryAcquire(weight int64) bool {
	w := p.clampWeight(weight)
	if !p.sem.TryAcquire(w) {
		return false
	}
	p.inUse.Add(w)
	return true
}

// Release returns weight units previously acquired from the pool.
func (p *Pool) Release(weight int64) {
	w := p.clampWeight(weight)
	p.inUse.Add(-w)
	p.sem.Release(w)
}

// Do acquires weight units, executes fn(ctx), and always releases the weight.
func (p *Pool) Do(ctx context.Context, weight int64, fn func(context.Context) error) error {
	if err := p.Acquire(ctx, weight); err != nil {
		return err
	}
	defer p.Release(weight)
	if fn == nil {
		return nil
	}
	return fn(ctx)
}
