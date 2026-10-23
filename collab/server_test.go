package collab

import (
	"encoding/json"
	"testing"

	"github.com/asano69/cmgo/authority"
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

func (r *recorder) Track(_ []authority.Change, _ []uint16, author any, _ int64) {
	r.calls++
	r.authors = append(r.authors, author)
}

func (r *recorder) Message() []byte { return r.extra }

const pushHi = `{"type":"push","version":0,"updates":[{"clientID":"a","changes":[[0,"hi"]]}]}`

func TestPushRejectsInvalidChanges(t *testing.T) {
	rec := &recorder{}
	s := New(WithTracker(rec))
	bad := `{"type":"push","version":0,"updates":[{"clientID":"a","changes":[3]}]}`

	if err := s.handle(newTestClient(), []byte(bad)); err == nil {
		t.Fatal("expected an error for changes that do not fit the document")
	}
	if s.Version() != 0 {
		t.Errorf("invalid update was recorded: version %d", s.Version())
	}
	if rec.calls != 0 {
		t.Errorf("Track was called %d times for an invalid update", rec.calls)
	}
}

func TestPushAppliesChanges(t *testing.T) {
	s := New()
	if err := s.handle(newTestClient(), []byte(pushHi)); err != nil {
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

	if err := s.handle(c, []byte(pushHi)); err != nil {
		t.Fatal(err)
	}
	if rec.calls != 1 || rec.authors[0] != "alice" {
		t.Errorf("Track calls = %d, authors = %v; want one call by alice", rec.calls, rec.authors)
	}
}

func TestPushSendsMissingUpdatesToAStaleClient(t *testing.T) {
	s := New()
	if err := s.handle(newTestClient(), []byte(pushHi)); err != nil {
		t.Fatal(err)
	}

	stale := newTestClient()
	push := `{"type":"push","version":0,"updates":[{"clientID":"b","changes":[[0,"x"]]}]}`
	if err := s.handle(stale, []byte(push)); err != nil {
		t.Fatal(err)
	}
	if len(stale.send) != 1 {
		t.Fatalf("stale client got %d messages, want 1", len(stale.send))
	}
	var msg updatesMessage
	if err := json.Unmarshal(<-stale.send, &msg); err != nil {
		t.Fatal(err)
	}
	if msg.Type != "updates" || msg.From != 0 || len(msg.Updates) != 1 {
		t.Errorf("got %+v, want the first update from index 0", msg)
	}
	if got := s.Text(); got != "hi" {
		t.Errorf("a stale push changed the text: %q", got)
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
	if s.Version() != 1 {
		t.Errorf("replacing with the same text added an update: %d", s.Version())
	}
}

func TestUpdatesAreEncodedOncePerBatch(t *testing.T) {
	s := New()
	a, b := newTestClient(), newTestClient()
	s.join(a)
	s.join(b)

	s.Replace("hello")
	fromA, fromB := <-a.send, <-b.send
	if &fromA[0] != &fromB[0] {
		t.Error("the batch was encoded once per client")
	}
}

func TestLeaveStopsTheUpdates(t *testing.T) {
	s := New()
	c := newTestClient()
	s.join(c)
	s.leave(c)

	s.Replace("hello")
	if len(c.send) != 0 {
		t.Errorf("a client that left got %d messages", len(c.send))
	}
}
