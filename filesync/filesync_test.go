package filesync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/asano69/cmgo/persistence"
)

const room = "data.txt"

var ctx = context.Background()

type fakeDoc struct{ text string }

func (d *fakeDoc) Text() string     { return d.text }
func (d *fakeDoc) Replace(t string) { d.text = t }

// newSyncer returns a Syncer over a file store, and the path of its file.
func newSyncer(t *testing.T) (*Syncer, *fakeDoc, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := persistence.NewFilePersistence(dir)
	if err != nil {
		t.Fatal(err)
	}
	doc := &fakeDoc{}
	s := &Syncer{doc: doc, store: store, room: room}
	return s, doc, filepath.Join(dir, room)
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func mustSync(t *testing.T, s *Syncer) {
	t.Helper()
	if err := s.sync(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestSyncLoadsExistingFile(t *testing.T) {
	s, doc, path := newSyncer(t)
	writeFile(t, path, "from file")
	mustSync(t, s)
	if doc.text != "from file" {
		t.Errorf("buffer = %q, want %q", doc.text, "from file")
	}
}

func TestSyncSavesBuffer(t *testing.T) {
	s, doc, path := newSyncer(t)
	mustSync(t, s)
	doc.text = "typed"
	mustSync(t, s)
	if got := readFile(t, path); got != "typed" {
		t.Errorf("file = %q, want %q", got, "typed")
	}
}

// A keystroke right after a save must not be mistaken for an external edit.
func TestSyncKeepsEditsMadeAfterSave(t *testing.T) {
	s, doc, path := newSyncer(t)
	doc.text = "a"
	mustSync(t, s)
	doc.text = "ab"
	mustSync(t, s)
	if doc.text != "ab" {
		t.Errorf("buffer = %q, want %q", doc.text, "ab")
	}
	if got := readFile(t, path); got != "ab" {
		t.Errorf("file = %q, want %q", got, "ab")
	}
}

func TestSyncExternalEditWins(t *testing.T) {
	s, doc, path := newSyncer(t)
	doc.text = "buffer"
	mustSync(t, s)

	doc.text = "unsaved buffer edit"
	writeFile(t, path, "external")
	mustSync(t, s)
	if doc.text != "external" {
		t.Errorf("buffer = %q, want %q", doc.text, "external")
	}
}

func TestSyncNormalizesLineEndingsWithoutRewriting(t *testing.T) {
	s, doc, path := newSyncer(t)
	writeFile(t, path, "a\r\nb")
	mustSync(t, s)
	if doc.text != "a\nb" {
		t.Errorf("buffer = %q, want %q", doc.text, "a\nb")
	}
	if got := readFile(t, path); got != "a\r\nb" {
		t.Errorf("file was rewritten: %q", got)
	}
}

func TestSyncRestoresDeletedFile(t *testing.T) {
	s, doc, path := newSyncer(t)
	doc.text = "keep me"
	mustSync(t, s)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	mustSync(t, s)
	if got := readFile(t, path); got != "keep me" {
		t.Errorf("file = %q, want %q", got, "keep me")
	}
}

// Any SnapshotStore works, not only files.
func TestSyncWorksWithAnyStore(t *testing.T) {
	store := persistence.NewMemoryPersistence()
	doc := &fakeDoc{text: "buffer"}
	s := &Syncer{doc: doc, store: store, room: "doc"}
	mustSync(t, s)

	got, err := store.Load(ctx, "doc")
	if err != nil || string(got) != "buffer" {
		t.Fatalf("stored %q, %v; want %q", got, err, "buffer")
	}

	if err := store.Store(ctx, "doc", []byte("external")); err != nil {
		t.Fatal(err)
	}
	mustSync(t, s)
	if doc.text != "external" {
		t.Errorf("buffer = %q, want %q", doc.text, "external")
	}
}
