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
