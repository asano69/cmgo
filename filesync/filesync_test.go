package filesync

import (
	"os"
	"path/filepath"
	"testing"
)

type fakeDoc struct{ text string }

func (d *fakeDoc) Text() string     { return d.text }
func (d *fakeDoc) Replace(t string) { d.text = t }

func newSyncer(t *testing.T) (*Syncer, *fakeDoc) {
	t.Helper()
	doc := &fakeDoc{}
	return &Syncer{doc: doc, path: filepath.Join(t.TempDir(), "data.txt")}, doc
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
	if err := s.sync(); err != nil {
		t.Fatal(err)
	}
}

func TestSyncLoadsExistingFile(t *testing.T) {
	s, doc := newSyncer(t)
	writeFile(t, s.path, "from file")
	mustSync(t, s)
	if doc.text != "from file" {
		t.Errorf("buffer = %q, want %q", doc.text, "from file")
	}
}

func TestSyncSavesBuffer(t *testing.T) {
	s, doc := newSyncer(t)
	mustSync(t, s)
	doc.text = "typed"
	mustSync(t, s)
	if got := readFile(t, s.path); got != "typed" {
		t.Errorf("file = %q, want %q", got, "typed")
	}
}

// A keystroke right after a save must not be mistaken for an external edit.
func TestSyncKeepsEditsMadeAfterSave(t *testing.T) {
	s, doc := newSyncer(t)
	doc.text = "a"
	mustSync(t, s)
	doc.text = "ab"
	mustSync(t, s)
	if doc.text != "ab" {
		t.Errorf("buffer = %q, want %q", doc.text, "ab")
	}
	if got := readFile(t, s.path); got != "ab" {
		t.Errorf("file = %q, want %q", got, "ab")
	}
}

func TestSyncExternalEditWins(t *testing.T) {
	s, doc := newSyncer(t)
	doc.text = "buffer"
	mustSync(t, s)

	doc.text = "unsaved buffer edit"
	writeFile(t, s.path, "external")
	mustSync(t, s)
	if doc.text != "external" {
		t.Errorf("buffer = %q, want %q", doc.text, "external")
	}
}

func TestSyncNormalizesLineEndingsWithoutRewriting(t *testing.T) {
	s, doc := newSyncer(t)
	writeFile(t, s.path, "a\r\nb")
	mustSync(t, s)
	if doc.text != "a\nb" {
		t.Errorf("buffer = %q, want %q", doc.text, "a\nb")
	}
	if got := readFile(t, s.path); got != "a\r\nb" {
		t.Errorf("file was rewritten: %q", got)
	}
}

func TestSyncRestoresDeletedFile(t *testing.T) {
	s, doc := newSyncer(t)
	doc.text = "keep me"
	mustSync(t, s)
	if err := os.Remove(s.path); err != nil {
		t.Fatal(err)
	}
	mustSync(t, s)
	if got := readFile(t, s.path); got != "keep me" {
		t.Errorf("file = %q, want %q", got, "keep me")
	}
}
