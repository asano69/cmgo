// Package authority is the authoritative copy of a shared document. It keeps
// the ordered list of accepted updates and the text that they produce, decides
// whether a client's push is accepted, and tells subscribers about every
// accepted update. It knows nothing about connections: collab serves it to
// browsers, and a program can also edit it directly with Edit and Replace.
package authority

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/asano69/cmgo/ot"
)

// serverID is the clientID of updates that the authority makes itself. It
// never matches a real client, so every client treats them as remote updates.
const serverID = "server"

// Update is one client's change set, as @codemirror/collab defines it.
type Update struct {
	ClientID string       `json:"clientID"`
	Changes  ot.ChangeSet `json:"changes"`
}

// Change is one update as applied: the document before it and its changes.
type Change struct {
	Before  []uint16
	Changes ot.ChangeSet
}

// Tracker follows the document. Track is called with the Document locked, so
// it must not call the Document.
type Tracker interface {
	// Track is called once the updates were applied. doc is the document
	// after all changes, author is what the caller of Push passed, or nil for
	// edits made by Edit and Replace. The slices must not be modified.
	Track(changes []Change, doc []uint16, author any, now int64)
}

type noTracker struct{}

func (noTracker) Track([]Change, []uint16, any, int64) {}

// Option configures a Document.
type Option func(*Document)

// WithTracker makes the Document report every change to t.
func WithTracker(t Tracker) Option {
	return func(d *Document) { d.tracker = t }
}

type subscriber struct {
	f func(from int, updates []Update)
}

// Document is one shared document. It is safe for concurrent use.
type Document struct {
	tracker Tracker

	mu      sync.Mutex
	updates []Update // the accepted updates; the version is their number
	doc     []uint16 // the document after all updates
	subs    map[*subscriber]struct{}
}

func New(opts ...Option) *Document {
	d := &Document{
		tracker: noTracker{},
		updates: []Update{},
		doc:     []uint16{},
		subs:    map[*subscriber]struct{}{},
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Text returns the current document.
func (d *Document) Text() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return ot.Decode(d.doc)
}

// Version returns the number of accepted updates.
func (d *Document) Version() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.updates)
}

// Inspect calls f with the current document while the Document is locked, so a
// Tracker can read it consistently. f must not modify doc or call the Document.
func (d *Document) Inspect(f func(doc []uint16)) {
	d.mu.Lock()
	defer d.mu.Unlock()
	f(d.doc)
}

// Subscribe calls f with the whole history of accepted updates (empty if there
// is none), then once for every batch accepted later, in order. from is the
// index of the first update of the batch. f is called with the Document
// locked, so it must not call the Document or block. Calling the returned
// function stops the calls.
func (d *Document) Subscribe(f func(from int, updates []Update)) (unsubscribe func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s := &subscriber{f: f}
	d.subs[s] = struct{}{}
	f(0, d.updates)
	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		delete(d.subs, s)
	}
}

// Push accepts updates that are based on version if that is the current
// version and they apply to the document. If the client is behind, nothing is
// applied and the updates it is missing are returned, so that it can rebase
// and push again. author is handed to the Tracker.
func (d *Document) Push(version int, updates []Update, author any) (missing []Update, err error) {
	if err := validate(updates); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	switch {
	case version < 0 || version > len(d.updates):
		return nil, fmt.Errorf("invalid version %d (server has %d updates)", version, len(d.updates))
	case version < len(d.updates):
		return d.updates[version:], nil
	case len(updates) > 0:
		return nil, d.accept(updates, author)
	}
	return nil, nil
}

// Edit applies changes that were made for the current document as an update
// from the server. Clients rebase their unconfirmed edits onto it.
func (d *Document) Edit(changes ot.ChangeSet) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.accept([]Update{{ClientID: serverID, Changes: changes}}, nil)
}

// Replace makes text the document. Only the part that differs is changed, so
// clients keep their cursors in the rest.
func (d *Document) Replace(text string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	changes := ot.Diff(d.doc, ot.Encode(text))
	if changes.Empty() {
		return
	}
	if err := d.accept([]Update{{ClientID: serverID, Changes: changes}}, nil); err != nil {
		// Unreachable: Diff only returns changes that fit d.doc.
		log.Printf("cmgo: replace: %v", err)
	}
}

func validate(updates []Update) error {
	for _, u := range updates {
		if u.ClientID == "" {
			return fmt.Errorf("update is missing clientID")
		}
	}
	return nil
}

// accept applies updates, reports them to the tracker, appends them to the
// history and tells the subscribers. Nothing changes if one of them does not
// apply. d.mu must be held.
func (d *Document) accept(updates []Update, author any) error {
	doc, changes, err := applyUpdates(d.doc, updates)
	if err != nil {
		return err
	}
	d.doc = doc
	d.tracker.Track(changes, doc, author, time.Now().Unix())

	from := len(d.updates)
	d.updates = append(d.updates, updates...)
	for s := range d.subs {
		s.f(from, updates)
	}
	return nil
}

// applyUpdates applies updates in order to doc and returns the new document
// with one Change per update. doc is not modified.
func applyUpdates(doc []uint16, updates []Update) ([]uint16, []Change, error) {
	changes := make([]Change, 0, len(updates))
	for i, u := range updates {
		next, err := u.Changes.Apply(doc)
		if err != nil {
			return nil, nil, fmt.Errorf("update %d: %w", i, err)
		}
		changes = append(changes, Change{Before: doc, Changes: u.Changes})
		doc = next
	}
	return doc, changes, nil
}
