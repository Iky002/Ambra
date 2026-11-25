package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBusSubscribePublish(t *testing.T) {
	bus := New()

	var wg sync.WaitGroup
	var count atomic.Int32
	var receivedPayload atomic.Value

	wg.Add(2)
	bus.Subscribe(EventLibraryChanged, func(ctx context.Context, e Event) {
		defer wg.Done()
		if e.Type != EventLibraryChanged {
			t.Errorf("unexpected event type %q", e.Type)
		}
		receivedPayload.Store(e.Payload)
		count.Add(1)
	})
	bus.Subscribe(EventLibraryChanged, func(ctx context.Context, e Event) {
		defer wg.Done()
		count.Add(1)
	})

	otherCalled := atomic.Bool{}
	bus.Subscribe(EventPlaybackStart, func(ctx context.Context, e Event) {
		otherCalled.Store(true)
	})

	bus.Publish(context.Background(), Event{
		Type:    EventLibraryChanged,
		Payload: "lib-1",
	})

	// Publishing to an event with no subscribers should not panic.
	bus.Publish(context.Background(), Event{Type: EventTaskProgress, Payload: 50})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event handlers")
	}

	if count.Load() != 2 {
		t.Fatalf("expected 2 handlers called, got %d", count.Load())
	}
	if got := receivedPayload.Load(); got != "lib-1" {
		t.Fatalf("expected payload lib-1, got %v", got)
	}
	if otherCalled.Load() {
		t.Fatal("unrelated event handler should not have been called")
	}
}

func TestBusConcurrentPublishSubscribe(t *testing.T) {
	bus := New()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			bus.Subscribe(EventSessionUpdated, func(ctx context.Context, e Event) {})
		}()
		go func() {
			defer wg.Done()
			bus.Publish(context.Background(), Event{Type: EventSessionUpdated})
		}()
	}
	wg.Wait()
}

func TestBusNilPanicRecoveryAndConcurrencyLimit(t *testing.T) {
	// 1. Nil bus Publish is a safe no-op.
	var nilBus *Bus
	nilBus.Publish(context.Background(), Event{Type: EventLibraryChanged, Payload: "ignored"})

	// 2. Handler panic recovery + nil context fallback + semaphore release.
	bus := New()
	var wg sync.WaitGroup
	var okCalled atomic.Bool
	wg.Add(2)
	bus.Subscribe(EventUserDataChanged, func(ctx context.Context, e Event) {
		defer wg.Done()
		panic("simulated handler crash")
	})
	bus.Subscribe(EventUserDataChanged, func(ctx context.Context, e Event) {
		defer wg.Done()
		if ctx == nil {
			t.Error("expected non-nil handler context")
		}
		okCalled.Store(true)
	})

	// Pass nil context to exercise default context.Background() fallback.
	bus.Publish(nil, Event{Type: EventUserDataChanged, Payload: "item-1"})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for handlers after panic recovery")
	}
	if !okCalled.Load() {
		t.Fatal("expected sibling handler to execute even when another handler panics")
	}

	// 3. Concurrency semaphore full drops event gracefully without blocking.
	limitedBus := &Bus{
		handlers: make(map[EventType][]Handler),
		sem:      make(chan struct{}, 1),
	}
	called := atomic.Bool{}
	limitedBus.Subscribe(EventPlaybackStop, func(ctx context.Context, e Event) {
		called.Store(true)
	})
	// Occupy the only slot.
	limitedBus.sem <- struct{}{}
	limitedBus.Publish(context.Background(), Event{Type: EventPlaybackStop})
	<-limitedBus.sem
	if called.Load() {
		t.Fatal("expected event to be dropped when semaphore is full")
	}
}
