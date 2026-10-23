// Package persistence defines where document snapshots are stored. A snapshot
// is the full content of a document at one moment; saving one replaces the
// previous snapshot of the same document.
package persistence

import (
	"context"
	"errors"
)

// ErrNotFound is returned (wrapped) by LoadSnapshot when nothing was saved
// for the document yet. Test for it with errors.Is.
var ErrNotFound = errors.New("snapshot not found")

// SnapshotStore saves and loads the snapshot of a document.
type SnapshotStore interface {
	// LoadSnapshot returns the latest snapshot of docID, or an error wrapping
	// ErrNotFound if there is none.
	LoadSnapshot(ctx context.Context, docID string) ([]byte, error)
	// SaveSnapshot replaces the snapshot of docID.
	SaveSnapshot(ctx context.Context, docID string, snapshot []byte) error
}

// Watcher is implemented by stores that can tell when a snapshot was changed
// by someone else. Consumers that find it on a store use it to react sooner
// than polling would.
type Watcher interface {
	// Watch sends a value whenever the snapshot of docID may have changed. The
	// channel is closed when ctx is done. Sends never block: values are
	// dropped while one is already pending.
	Watch(ctx context.Context, docID string) (<-chan struct{}, error)
}
