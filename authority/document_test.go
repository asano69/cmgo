package authority

import (
	"slices"
	"testing"

	"github.com/asano69/cmgo/ot"
)

// recorder is a Tracker that remembers what it was told.
type recorder struct {
	calls   int
	authors []any
}

func (r *recorder) Track(_ []Change, _ []uint16, author any, _ int64) {
	r.calls++
	r.authors = append(r.authors, author)
}

// insertAt0 returns the changes that insert text at the start of a document of
// docLen units.
func insertAt0(t *testing.T, docLen int, text string) ot.ChangeSet {
	t.Helper()
	cs, err := ot.NewChangeSet(docLen, ot.Edit{Text: ot.Encode(text)})
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

func hi(t *testing.T, clientID string) Update {
	return Update{ClientID: clientID, Changes: insertAt0(t, 0, "hi")}
}

func TestPushRejectsInvalidChanges(t *testing.T) {
	rec := &recorder{}
	d := New(WithTracker(rec))
	bad, err := ot.NewChangeSet(3) // covers 3 units, the document has none
	if err != nil {
		t.Fatal(err)
	}

	if _, err := d.Push(0, []Update{{ClientID: "a", Changes: bad}}, nil); err == nil {
		t.Fatal("expected an error for changes that do not fit the document")
	}
	if d.Version() != 0 {
		t.Errorf("invalid update was recorded: version %d", d.Version())
	}
	if rec.calls != 0 {
		t.Errorf("Track was called %d times for an invalid update", rec.calls)
	}
}

func TestPushRejectsUpdatesWithoutClientID(t *testing.T) {
	d := New()
	if _, err := d.Push(0, []Update{hi(t, "")}, nil); err == nil {
		t.Fatal("expected an error for an update without clientID")
	}
}

func TestPushRejectsUnknownVersions(t *testing.T) {
	d := New()
	for _, v := range []int{-1, 1} {
		if _, err := d.Push(v, nil, nil); err == nil {
			t.Errorf("version %d was accepted", v)
		}
	}
}

func TestPushAppliesChanges(t *testing.T) {
	d := New()
	if _, err := d.Push(0, []Update{hi(t, "a")}, nil); err != nil {
		t.Fatal(err)
	}
	if got := d.Text(); got != "hi" {
		t.Errorf("Text() = %q, want %q", got, "hi")
	}
	if d.Version() != 1 {
		t.Errorf("Version() = %d, want 1", d.Version())
	}
}

func TestPushTracksAppliedChangesWithTheAuthor(t *testing.T) {
	rec := &recorder{}
	d := New(WithTracker(rec))

	if _, err := d.Push(0, []Update{hi(t, "a")}, "alice"); err != nil {
		t.Fatal(err)
	}
	if rec.calls != 1 || rec.authors[0] != "alice" {
		t.Errorf("Track calls = %d, authors = %v; want one call by alice", rec.calls, rec.authors)
	}
}

func TestPushReturnsMissingUpdatesToAStaleClient(t *testing.T) {
	d := New()
	if _, err := d.Push(0, []Update{hi(t, "a")}, nil); err != nil {
		t.Fatal(err)
	}

	missing, err := d.Push(0, []Update{{ClientID: "b", Changes: insertAt0(t, 0, "x")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 1 || missing[0].ClientID != "a" {
		t.Errorf("missing = %v, want the update of a", missing)
	}
	if d.Text() != "hi" || d.Version() != 1 {
		t.Errorf("a stale push changed the document: %q, version %d", d.Text(), d.Version())
	}
}

func TestEdit(t *testing.T) {
	d := New()
	if err := d.Edit(insertAt0(t, 0, "hi")); err != nil {
		t.Fatal(err)
	}
	if got := d.Text(); got != "hi" {
		t.Errorf("Text() = %q, want %q", got, "hi")
	}
	if err := d.Edit(insertAt0(t, 0, "x")); err == nil {
		t.Error("changes for another document length were accepted")
	}
}

type call struct{ from, n int }

func TestReplaceUpdatesTextAndNotifiesSubscribers(t *testing.T) {
	rec := &recorder{}
	d := New(WithTracker(rec))
	var calls []call
	d.Subscribe(func(from int, updates []Update) { calls = append(calls, call{from, len(updates)}) })

	d.Replace("hello")
	if got := d.Text(); got != "hello" {
		t.Errorf("Text() = %q, want %q", got, "hello")
	}
	// The replay of the empty history, then the replacement.
	if want := []call{{0, 0}, {0, 1}}; !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
	if rec.calls != 1 || rec.authors[0] != nil {
		t.Errorf("Track calls = %d, authors = %v; want one call without author", rec.calls, rec.authors)
	}

	d.Replace("hello")
	if d.Version() != 1 {
		t.Errorf("replacing with the same text added an update: %d", d.Version())
	}
}

func TestSubscribeReplaysTheHistory(t *testing.T) {
	d := New()
	d.Replace("a")
	d.Replace("ab")

	var calls []call
	d.Subscribe(func(from int, updates []Update) { calls = append(calls, call{from, len(updates)}) })
	if want := []call{{0, 2}}; !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
}

func TestUnsubscribeStopsTheCalls(t *testing.T) {
	d := New()
	n := 0
	unsubscribe := d.Subscribe(func(int, []Update) { n++ })
	unsubscribe()

	d.Replace("a")
	if n != 1 { // only the replay on subscribing
		t.Errorf("subscriber was called %d times, want 1", n)
	}
}
