package eventbus

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type EventType string

const (
	EventLibraryChanged     EventType = "LibraryChanged"
	EventUserDataChanged    EventType = "UserDataChanged"
	EventPlaybackStart      EventType = "PlaybackStart"
	EventPlaybackProgress   EventType = "PlaybackProgress"
	EventPlaybackStop       EventType = "PlaybackStop"
	EventSessionUpdated     EventType = "SessionUpdated"
	EventTaskProgress       EventType = "TaskProgress"
	EventActivityLogCreated EventType = "ActivityLogCreated"

	defaultMaxConcurrentHandlers = 256
	defaultHandlerTimeout        = 30 * time.Second
)

type Event struct {
	Type    EventType
	Payload any
}

type Handler func(ctx context.Context, e Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler
	sem      chan struct{}
}

func New() *Bus {
	return &Bus{
		handlers: make(map[EventType][]Handler),
		sem:      make(chan struct{}, defaultMaxConcurrentHandlers),
	}
}

func (b *Bus) Subscribe(eventType EventType, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], h)
}

func (b *Bus) Publish(ctx context.Context, e Event) {
	if b == nil {
		return
	}
	b.mu.RLock()
	hs := append([]Handler(nil), b.handlers[e.Type]...)
	b.mu.RUnlock()
	baseCtx := context.Background()
	if ctx != nil {
		baseCtx = context.WithoutCancel(ctx)
	}
	for _, h := range hs {
		fn := h
		select {
		case b.sem <- struct{}{}:
			go func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("eventbus handler panicked", "event", e.Type, "panic", r)
					}
					<-b.sem
				}()
				hCtx, cancel := context.WithTimeout(baseCtx, defaultHandlerTimeout)
				defer cancel()
				fn(hCtx, e)
			}()
		default:
			slog.Warn("eventbus concurrency limit reached, dropping event", "event", e.Type)
		}
	}
}
