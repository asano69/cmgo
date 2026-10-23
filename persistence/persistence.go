// Package persistence defines where the state of shared documents is stored,
// keyed by room name as in ygo's persistence package. A store holds the head
// state of each room, which is the full text of the document. There is no
// history: storing replaces what was stored before.
package persistence

import (
	"context"
	"errors"
)

// ErrRoomNotFound is returned (wrapped) by Load when nothing was stored for
// the room yet. Test for it with errors.Is.
var ErrRoomNotFound = errors.New("persistence: room not found")

// Persistence stores the head state of rooms.
type Persistence interface {
	// Load returns the stored state of room, or an error wrapping
	// ErrRoomNotFound if there is none.
	Load(ctx context.Context, room string) ([]byte, error)
	// Store replaces the state of room.
	Store(ctx context.Context, room string, state []byte) error
}

// Watcher is implemented by stores that can tell when the state of a room was
// changed by someone else. Consumers that find it on a store use it to react
// sooner than polling would.
type Watcher interface {
	// Watch sends a value whenever the state of room may have changed. The
	// channel is closed when ctx is done. Sends never block: values are
	// dropped while one is already pending.
	Watch(ctx context.Context, room string) (<-chan struct{}, error)
}
