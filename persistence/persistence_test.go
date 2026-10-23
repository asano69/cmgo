package persistence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var ctx = context.Background()

func stores(t *testing.T) map[string]SnapshotStore {
	return map[string]SnapshotStore{
		"memory": NewMemoryStore(),
		"file":   NewFileStore(t.TempDir()),
	}
}

func TestLoadMissingSnapshot(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := s.LoadSnapshot(ctx, "doc"); !errors.Is(err, ErrNotFound) {
				t.Errorf("error = %v, want ErrNotFound", err)
			}
		})
	}
}

func TestSaveReplacesTheSnapshot(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			for _, text := range []string{"first", "second"} {
				if err := s.SaveSnapshot(ctx, "doc", []byte(text)); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.LoadSnapshot(ctx, "doc")
			if err != nil || string(got) != "second" {
				t.Errorf("got %q, %v; want %q", got, err, "second")
			}
		})
	}
}

func TestMemoryStoreCopiesSnapshots(t *testing.T) {
	s := NewMemoryStore()
	data := []byte("abc")
	if err := s.SaveSnapshot(ctx, "doc", data); err != nil {
		t.Fatal(err)
	}
	data[0] = 'X'
	got, _ := s.LoadSnapshot(ctx, "doc")
	if string(got) != "abc" {
		t.Errorf("the caller changed the stored snapshot: %q", got)
	}
}

func TestFileStoreRefusesIDsOutsideTheDirectory(t *testing.T) {
	s := NewFileStore(t.TempDir())
	for _, id := range []string{"", ".", "..", "../x", "a/b"} {
		if err := s.SaveSnapshot(ctx, id, nil); err == nil {
			t.Errorf("document ID %q was accepted", id)
		}
	}
}

func TestFileStoreWatchReportsEdits(t *testing.T) {
	dir := t.TempDir()
	s := NewFileStore(dir)
	wctx, cancel := context.WithCancel(ctx)
	defer cancel()
	changed, err := s.Watch(wctx, "doc")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "doc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(2 * time.Second):
		t.Fatal("no notification for an edit of the file")
	}
}
