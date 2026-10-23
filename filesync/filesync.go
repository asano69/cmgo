// Package filesync keeps a Document and its stored snapshot in sync. The
// document is saved to the store every second when it changed, and an edit
// made to the snapshot by someone else always replaces the document.
package filesync

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/asano69/cmgo/persistence"
)

const (
	saveInterval  = time.Second
	debounceDelay = 100 * time.Millisecond
)

// Document is the edit buffer that is kept in sync with the store.
type Document interface {
	Text() string
	Replace(text string)
}

// Syncer synchronizes one Document with one snapshot of a store.
type Syncer struct {
	doc   Document
	store persistence.Persistence
	room string

	// mu serializes sync calls. Lock order: mu, then the document's own lock.
	mu sync.Mutex
	// lastSynced is the text that the snapshot and the buffer last agreed on,
	// in the buffer's line ending form. A snapshot that still holds it was not
	// edited by anyone else, however far the buffer has moved on since.
	lastSynced string
}

// Start loads the state of room from store into doc (or creates it from doc),
// then keeps both in sync in the background until ctx is done. If the store
// implements persistence.Watcher, edits of the stored state are picked up at
// once; otherwise at the next save interval.
func Start(ctx context.Context, doc Document, store persistence.Persistence, room string) error {
	s := &Syncer{doc: doc, store: store, room: room}
	if err := s.sync(ctx); err != nil {
		return err
	}

	var changed <-chan struct{}
	if w, ok := store.(persistence.Watcher); ok {
		var err error
		if changed, err = w.Watch(ctx, room); err != nil {
			return err
		}
	}
	go s.run(ctx, changed)
	return nil
}

// run syncs on every tick and after every notification. changed may be nil.
func (s *Syncer) run(ctx context.Context, changed <-chan struct{}) {
	tick := time.NewTicker(saveInterval)
	defer tick.Stop()

	// Each notification restarts the delay, so one burst of events syncs once
	// and a half-written file is not read.
	var debounce <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.syncLogged(ctx)
		case _, ok := <-changed:
			if !ok {
				changed = nil
				continue
			}
			debounce = time.After(debounceDelay)
		case <-debounce:
			debounce = nil
			s.syncLogged(ctx)
		}
	}
}

func (s *Syncer) syncLogged(ctx context.Context) {
	if err := s.sync(ctx); err != nil && ctx.Err() == nil {
		log.Printf("filesync: %v", err)
	}
}

// sync brings snapshot and buffer together. A snapshot that differs from
// lastSynced was edited by someone else and wins; otherwise the buffer is
// saved if it moved.
func (s *Syncer) sync(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored, exists, err := s.load(ctx)
	if err != nil {
		return err
	}
	if exists && stored != s.lastSynced {
		s.lastSynced = stored
		s.doc.Replace(stored)
		return nil
	}

	text := s.doc.Text()
	if exists && text == s.lastSynced {
		return nil
	}
	// Also reached when the snapshot is missing, which restores it.
	if err := s.store.Store(ctx, s.room, []byte(text)); err != nil {
		return fmt.Errorf("save %s: %w", s.room, err)
	}
	s.lastSynced = text
	return nil
}

// lineEndings converts to the "\n" form that the editor uses.
var lineEndings = strings.NewReplacer("\r\n", "\n", "\r", "\n")

func (s *Syncer) load(ctx context.Context) (text string, exists bool, err error) {
	data, err := s.store.Load(ctx, s.room)
	if errors.Is(err, persistence.ErrRoomNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load %s: %w", s.room, err)
	}
	return lineEndings.Replace(string(data)), true, nil
}
