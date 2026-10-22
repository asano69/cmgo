// Package collab is a reusable server for @codemirror/collab. It keeps the
// ordered list of accepted updates of one document, applies them to the text,
// relays them to every connected client and relays remote cursors.
//
// Typical use:
//
//	mux.Handle("/connection/collab", collab.New())
//
// Serve several documents with several Servers. To authenticate connections,
// wrap the Server in an http.Handler. Use WithTracker to follow the edits
// (for example to keep metadata per line) and WithAuthor to tell who made them.
package collab

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// serverID is the clientID of updates that the server makes itself. It never
// matches a real client, so every client treats them as remote updates.
const serverID = "server"

// Update is one client's change set. Changes is kept opaque in the history:
// it is relayed as is.
type Update struct {
	ClientID string          `json:"clientID"`
	Changes  json.RawMessage `json:"changes"`
}

// Edit replaces the units From..To of a document with Text. Positions are in
// UTF-16 code units, like CodeMirror's.
type Edit struct {
	From, To int
	Text     []uint16
}

// Change is one update as applied: the document before it and its edits.
type Change struct {
	Before []uint16
	Edits  []Edit
}

// Tracker follows the document. Both methods are called with the Server
// locked, so they must not call the Server.
type Tracker interface {
	// Track is called once the updates were applied. doc is the document
	// after all changes, author is what WithAuthor returned for the
	// connection, or nil for edits made by Replace. The slices must not be
	// modified.
	Track(changes []Change, doc []uint16, author any, now int64)
	// Message returns an extra message that is sent to a client when it joins
	// and to every client after each accepted update. nil means none.
	Message() []byte
}

type noTracker struct{}

func (noTracker) Track([]Change, []uint16, any, int64) {}
func (noTracker) Message() []byte                      { return nil }

// Option configures a Server.
type Option func(*Server)

// WithTracker makes the Server report every change to t.
func WithTracker(t Tracker) Option {
	return func(s *Server) { s.tracker = t }
}

// WithAuthor decides who is behind a connection. The result is handed to
// Tracker.Track unchanged for every update of that connection.
func WithAuthor(f func(r *http.Request) any) Option {
	return func(s *Server) { s.authorOf = f }
}

// Server holds one shared document and is an http.Handler that serves the
// WebSocket connections editing it.
type Server struct {
	tracker  Tracker
	authorOf func(r *http.Request) any

	mu      sync.Mutex
	updates []Update
	doc     []uint16 // the document after all updates, in UTF-16 code units
	clients map[*client]struct{}
}

func New(opts ...Option) *Server {
	s := &Server{
		tracker:  noTracker{},
		authorOf: func(*http.Request) any { return nil },
		updates:  []Update{},
		doc:      []uint16{},
		clients:  map[*client]struct{}{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Text returns the current document.
func (s *Server) Text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fromUnits(s.doc)
}

// Inspect calls f with the current document while the Server is locked, so a
// Tracker can read it consistently. f must not modify doc or call the Server.
func (s *Server) Inspect(f func(doc []uint16)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s.doc)
}

// Replace makes text the document. The difference is recorded as an update
// from the server, so connected clients rebase their unconfirmed edits onto it.
func (s *Server) Replace(text string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changes, ok := replaceChanges(s.doc, toUnits(text))
	if !ok {
		return
	}
	updates := []Update{{ClientID: serverID, Changes: changes}}
	if err := s.apply(updates, nil); err != nil {
		// Unreachable: replaceChanges only returns changes that fit s.doc.
		log.Printf("cmgo: replace: %v", err)
		return
	}
	s.accept(updates)
}

// apply applies updates to the document and reports them to the tracker.
// Nothing changes if one of them fails. s.mu must be held.
func (s *Server) apply(updates []Update, author any) error {
	doc, changes, err := applyUpdates(s.doc, updates)
	if err != nil {
		return err
	}
	s.doc = doc
	s.tracker.Track(changes, doc, author, time.Now().Unix())
	return nil
}

// updatesMessage is sent by the server: updates starting at index From.
type updatesMessage struct {
	Type    string   `json:"type"`
	From    int      `json:"from"`
	Updates []Update `json:"updates"`
}

func encode(from int, updates []Update) []byte {
	data, err := json.Marshal(updatesMessage{Type: "updates", From: from, Updates: updates})
	if err != nil {
		// Unreachable: every Update was already parsed from valid JSON.
		log.Printf("cmgo: encode: %v", err)
		return nil
	}
	return data
}

// deliver queues data for c. A client that cannot keep up is dropped; it can
// reconnect and replay the whole history. nil data is ignored.
func deliver(c *client, data []byte) {
	if data == nil {
		return
	}
	select {
	case c.send <- data:
	default:
		c.cancel()
	}
}

func (s *Server) join(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c] = struct{}{}
	if len(s.updates) > 0 {
		deliver(c, encode(0, s.updates))
	}
	deliver(c, s.tracker.Message())
	s.sendCursors(c)
}

func (s *Server) leave(c *client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c)
	s.announceLeave(c)
}

func validate(updates []Update) error {
	for _, u := range updates {
		if u.ClientID == "" || len(u.Changes) == 0 {
			return fmt.Errorf("update is missing clientID or changes")
		}
	}
	return nil
}

// pushMessage is sent by a client: its synced version plus unconfirmed updates.
type pushMessage struct {
	Version int      `json:"version"`
	Updates []Update `json:"updates"`
}

// push accepts the updates if the client is up to date and they apply to the
// document, otherwise it sends the client what it is missing so that it can
// rebase and retry.
func (s *Server) push(c *client, msg pushMessage) error {
	if err := validate(msg.Updates); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	switch {
	case msg.Version < 0 || msg.Version > len(s.updates):
		return fmt.Errorf("invalid version %d (server has %d updates)", msg.Version, len(s.updates))
	case msg.Version < len(s.updates):
		deliver(c, encode(msg.Version, s.updates[msg.Version:]))
	case len(msg.Updates) > 0:
		if err := s.apply(msg.Updates, c.author); err != nil {
			return err
		}
		s.accept(msg.Updates)
	}
	return nil
}

// accept appends already applied updates to the history and relays them to
// every client, followed by the tracker's message: a client then has the text
// that the message belongs to. s.mu must be held.
func (s *Server) accept(updates []Update) {
	from := len(s.updates)
	s.updates = append(s.updates, updates...)
	data := encode(from, updates)
	extra := s.tracker.Message()
	for cl := range s.clients {
		deliver(cl, data)
		deliver(cl, extra)
	}
}

type envelope struct {
	Type string `json:"type"`
}

// handle dispatches one client message by its type.
func (s *Server) handle(c *client, data []byte) error {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return fmt.Errorf("invalid message: %w", err)
	}
	switch env.Type {
	case "push":
		var msg pushMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return fmt.Errorf("invalid push message: %w", err)
		}
		return s.push(c, msg)
	case "cursor":
		return s.cursor(c, data)
	default:
		return fmt.Errorf("unknown message type %q", env.Type)
	}
}
