// Package collab serves an authority.Document to browsers that use
// @codemirror/collab. It owns the connections: it relays accepted updates and
// remote cursors to every client, and hands pushes to the Document, which
// decides what is accepted.
//
// Typical use:
//
//	mux.Handle("/connection/collab", collab.New())
//
// The Server embeds its Document, so Text, Replace, Edit and Inspect edit the
// text that the clients see. Serve several documents with several Servers. To
// authenticate connections, wrap the Server in an http.Handler. Use WithTracker
// to follow the edits (for example to keep metadata per line) and WithAuthor
// to tell who made them.
package collab

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/asano69/cmgo/authority"
)

// Tracker follows the document and adds a message of its own to the stream.
type Tracker interface {
	authority.Tracker
	// Message returns an extra message that is sent to a client when it joins
	// and to every client after each accepted update. nil means none.
	Message() []byte
}

type noTracker struct{}

func (noTracker) Track([]authority.Change, []uint16, any, int64) {}
func (noTracker) Message() []byte                                { return nil }

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
	*authority.Document
	tracker  Tracker
	authorOf func(r *http.Request) any

	// mu guards clients and the id and cursor of every client. It is never
	// held while the Document is called.
	mu      sync.Mutex
	clients map[*client]struct{}
}

func New(opts ...Option) *Server {
	s := &Server{
		tracker:  noTracker{},
		authorOf: func(*http.Request) any { return nil },
		clients:  map[*client]struct{}{},
	}
	for _, opt := range opts {
		opt(s)
	}
	s.Document = authority.New(authority.WithTracker(s.tracker))
	return s
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

// join subscribes c to the document and gives it the cursors of everyone
// already connected. The subscription starts with the history, then the
// tracker's message, so a client has the text that the message belongs to.
func (s *Server) join(c *client) {
	c.unsubscribe = s.Subscribe(func(from int, updates []authority.Update) {
		if len(updates) > 0 {
			deliver(c, encode(from, updates))
		}
		deliver(c, s.tracker.Message())
	})

	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[c] = struct{}{}
	s.sendCursors(c)
}

func (s *Server) leave(c *client) {
	c.unsubscribe()

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, c)
	s.announceLeave(c)
}

// push hands the updates of a client to the document. A client that is behind
// is sent what it is missing, so that it can rebase and retry.
func (s *Server) push(c *client, msg pushMessage) error {
	missing, err := s.Push(msg.Version, msg.Updates, c.author)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		deliver(c, encode(msg.Version, missing))
	}
	return nil
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
