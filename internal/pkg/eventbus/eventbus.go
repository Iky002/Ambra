package eventbus

import (
	"context"
	"sync"
)

type EventType string

const (
	EventLibraryChanged  EventType = "LibraryChanged"
	EventUserDataChanged EventType = "UserDataChanged"
	EventPlaybackStart   EventType = "PlaybackStart"
	EventPlaybackProgress EventType = "PlaybackProgress"
	EventPlaybackStop    EventType = "PlaybackStop"
	EventSessionUpdated  EventType = "SessionUpdated"
	EventTaskProgress    EventType = "TaskProgress"
)

type Event struct {
	Type    EventType
	Payload any
}

type Handler func(ctx context.Context, e Event)

type Bus struct {
	mu       sync.RWMutex
	handlers map[EventType][]Handler
}

func New() *Bus {
	return &Bus{
		handlers: make(map[EventType][]Handler),
	}
}

func (b *Bus) Subscribe(eventType EventType, h Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], h)
}

func (b *Bus) Publish(ctx context.Context, e Event) {
	b.mu.RLock()
	hs := append([]Handler(nil), b.handlers[e.Type]...)
	b.mu.RUnlock()
	for _, h := range hs {
		go h(ctx, e)
	}
}
