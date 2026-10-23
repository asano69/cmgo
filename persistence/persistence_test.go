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

func newFile(t *testing.T, dir string) *FilePersistence {
	t.Helper()
	f, err := NewFilePersistence(dir)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func stores(t *testing.T) map[string]Persistence {
	return map[string]Persistence{
		"memory": NewMemoryPersistence(),
		"file":   newFile(t, t.TempDir()),
	}
}

func TestLoadMissingRoom(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			if _, err := s.Load(ctx, "doc"); !errors.Is(err, ErrRoomNotFound) {
				t.Errorf("error = %v, want ErrRoomNotFound", err)
			}
		})
	}
}

func TestStoreReplacesTheState(t *testing.T) {
	for name, s := range stores(t) {
		t.Run(name, func(t *testing.T) {
			for _, text := range []string{"first", "second"} {
				if err := s.Store(ctx, "doc", []byte(text)); err != nil {
					t.Fatal(err)
				}
			}
			got, err := s.Load(ctx, "doc")
			if err != nil || string(got) != "second" {
				t.Errorf("got %q, %v; want %q", got, err, "second")
			}
		})
	}
}

func TestMemoryPersistenceCopiesStates(t *testing.T) {
	s := NewMemoryPersistence()
	data := []byte("abc")
	if err := s.Store(ctx, "doc", data); err != nil {
		t.Fatal(err)
	}
	data[0] = 'X'
	got, _ := s.Load(ctx, "doc")
	if string(got) != "abc" {
		t.Errorf("the caller changed the stored state: %q", got)
	}
}

func TestNewFilePersistenceCreatesTheDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	newFile(t, dir)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("directory was not created: %v", err)
	}
}

func TestFilePersistenceRefusesRoomsOutsideTheDirectory(t *testing.T) {
	s := newFile(t, t.TempDir())
	for _, room := range []string{"", ".", "..", "../x", "a/b"} {
		if err := s.Store(ctx, room, nil); err == nil {
			t.Errorf("room %q was accepted", room)
		}
	}
}

func TestFilePersistenceWatchReportsEdits(t *testing.T) {
	dir := t.TempDir()
	s := newFile(t, dir)
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
