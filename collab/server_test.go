package cmgo

import (
	"encoding/json"
	"testing"
)

func newTestClient() *client {
	return &client{send: make(chan []byte, 8), cancel: func() {}}
}

// recorder is a Tracker that remembers what it was told.
type recorder struct {
	calls   int
	authors []any
	extra   []byte
}

func (r *recorder) Track(_ []Change, _ []uint16, author any, _ int64) {
	r.calls++
	r.authors = append(r.authors, author)
}

func (r *recorder) Message() []byte { return r.extra }

func TestPushRejectsInvalidChanges(t *testing.T) {
	rec := &recorder{}
	s := New(WithTracker(rec))
	bad := []Update{{ClientID: "a", Changes: json.RawMessage(`[3]`)}}

	if err := s.push(newTestClient(), pushMessage{Version: 0, Updates: bad}); err == nil {
		t.Fatal("expected an error for changes that do not fit the document")
	}
	if len(s.updates) != 0 {
		t.Errorf("invalid update was recorded: %d updates", len(s.updates))
	}
	if rec.calls != 0 {
		t.Errorf("Track was called %d times for an invalid update", rec.calls)
	}
}

func TestPushAppliesChanges(t *testing.T) {
	s := New()
	good := []Update{{ClientID: "a", Changes: json.RawMessage(`[[0,"hi"]]`)}}

	if err := s.push(newTestClient(), pushMessage{Version: 0, Updates: good}); err != nil {
		t.Fatal(err)
	}
	if got := s.Text(); got != "hi" {
		t.Errorf("Text() = %q, want %q", got, "hi")
	}
}

func TestPushTracksAppliedChangesWithTheAuthor(t *testing.T) {
	rec := &recorder{}
	s := New(WithTracker(rec))
	c := newTestClient()
	c.author = "alice"
	good := []Update{{ClientID: "a", Changes: json.RawMessage(`[[0,"hi"]]`)}}

	if err := s.push(c, pushMessage{Version: 0, Updates: good}); err != nil {
		t.Fatal(err)
	}
	if rec.calls != 1 || rec.authors[0] != "alice" {
		t.Errorf("Track calls = %d, authors = %v; want one call by alice", rec.calls, rec.authors)
	}
}

func TestReplaceUpdatesTextAndNotifiesClients(t *testing.T) {
	rec := &recorder{extra: []byte(`{"type":"extra"}`)}
	s := New(WithTracker(rec))
	c := newTestClient()
	s.join(c)
	joined := len(c.send) // the tracker's message that every client gets on joining

	s.Replace("hello")
	if got := s.Text(); got != "hello" {
		t.Errorf("Text() = %q, want %q", got, "hello")
	}
	// The update and the tracker's message that goes with it.
	if got := len(c.send) - joined; got != 2 {
		t.Errorf("client got %d messages, want 2", got)
	}
	if rec.calls != 1 || rec.authors[0] != nil {
		t.Errorf("Track calls = %d, authors = %v; want one call without author", rec.calls, rec.authors)
	}

	s.Replace("hello")
	if len(s.updates) != 1 {
		t.Errorf("replacing with the same text added an update: %d", len(s.updates))
	}
}
