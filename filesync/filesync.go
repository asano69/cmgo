// Package filesync keeps a Document and a text file in sync. The document is
// saved to the file every second when it changed, and an edit made to the file
// by another process always replaces the document.
package filesync

import (
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	saveInterval  = time.Second
	debounceDelay = 100 * time.Millisecond
)

// Document is the edit buffer that is kept in sync with the file.
type Document interface {
	Text() string
	Replace(text string)
}

// Syncer synchronizes one Document with one file.
type Syncer struct {
	doc  Document
	path string

	// mu serializes sync calls. Lock order: mu, then the document's own lock.
	mu sync.Mutex
	// lastSynced is the text that the file and the buffer last agreed on, in
	// the buffer's line ending form. A file that still holds it was not edited
	// by anyone else, however far the buffer has moved on since.
	lastSynced string
}

// Start loads path into doc (or creates it from doc), then keeps both in sync
// in the background for the lifetime of the process.
func Start(doc Document, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", path, err)
	}
	s := &Syncer{doc: doc, path: abs}
	if err := s.sync(); err != nil {
		return err
	}

	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("watch %s: %w", abs, err)
	}
	// The directory is watched, not the file: editors that save by renaming
	// and our own atomic writes replace the file, which ends a file watch.
	if err := w.Add(filepath.Dir(abs)); err != nil {
		w.Close()
		return fmt.Errorf("watch %s: %w", filepath.Dir(abs), err)
	}
	go s.run(w)
	return nil
}

func (s *Syncer) run(w *fsnotify.Watcher) {
	defer w.Close()
	tick := time.NewTicker(saveInterval)
	defer tick.Stop()

	// Each event restarts the delay, so one burst of events syncs once and
	// a half-written file is not read.
	var debounce <-chan time.Time
	for {
		select {
		case <-tick.C:
			s.syncLogged()
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if filepath.Clean(ev.Name) == s.path {
				debounce = time.After(debounceDelay)
			}
		case <-debounce:
			debounce = nil
			s.syncLogged()
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			log.Printf("filesync: watch %s: %v", s.path, err)
		}
	}
}

func (s *Syncer) syncLogged() {
	if err := s.sync(); err != nil {
		log.Printf("filesync: %v", err)
	}
}

// sync brings file and buffer together. A file that differs from lastSynced was
// edited by someone else and wins; otherwise the buffer is saved if it moved.
func (s *Syncer) sync() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fileText, exists, err := s.read()
	if err != nil {
		return err
	}
	if exists && fileText != s.lastSynced {
		s.lastSynced = fileText
		s.doc.Replace(fileText)
		return nil
	}

	text := s.doc.Text()
	if exists && text == s.lastSynced {
		return nil
	}
	// Also reached when the file is missing, which restores it.
	if err := WriteAtomic(s.path, text); err != nil {
		return err
	}
	s.lastSynced = text
	return nil
}

// lineEndings converts to the "\n" form that the editor uses.
var lineEndings = strings.NewReplacer("\r\n", "\n", "\r", "\n")

func (s *Syncer) read() (text string, exists bool, err error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read %s: %w", s.path, err)
	}
	return lineEndings.Replace(string(data)), true, nil
}

// WriteAtomic replaces path through a temporary file in the same directory, so
// other processes never see a half-written file.
func WriteAtomic(path, text string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sync-*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	_, err = tmp.WriteString(text)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
