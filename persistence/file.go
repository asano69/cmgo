package persistence

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// FileStore keeps the snapshot of a document in the file dir/docID, so that
// other programs can read and edit it. It implements Watcher.
type FileStore struct {
	dir string
}

func NewFileStore(dir string) *FileStore {
	return &FileStore{dir: filepath.Clean(dir)}
}

// path returns the file of docID. A docID is a plain file name: anything that
// could leave dir is refused.
func (s *FileStore) path(docID string) (string, error) {
	if docID == "" || docID == "." || docID == ".." || filepath.Base(docID) != docID {
		return "", fmt.Errorf("document ID %q must be a plain file name", docID)
	}
	return filepath.Join(s.dir, docID), nil
}

func (s *FileStore) LoadSnapshot(ctx context.Context, docID string) ([]byte, error) {
	p, err := s.path(docID)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, p)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return data, nil
}

func (s *FileStore) SaveSnapshot(ctx context.Context, docID string, snapshot []byte) error {
	p, err := s.path(docID)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return WriteAtomic(p, snapshot)
}

// Watch reports every event on the file of docID. The directory is watched,
// not the file: editors that save by renaming and WriteAtomic replace the
// file, which ends a file watch.
func (s *FileStore) Watch(ctx context.Context, docID string) (<-chan struct{}, error) {
	p, err := s.path(docID)
	if err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch %s: %w", p, err)
	}
	if err := w.Add(s.dir); err != nil {
		w.Close()
		return nil, fmt.Errorf("watch %s: %w", s.dir, err)
	}

	changed := make(chan struct{}, 1)
	go func() {
		defer w.Close()
		defer close(changed)
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if filepath.Clean(ev.Name) != p {
					continue
				}
				select {
				case changed <- struct{}{}:
				default: // one is pending already
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				log.Printf("persistence: watch %s: %v", p, err)
			}
		}
	}()
	return changed, nil
}

// WriteAtomic replaces path through a temporary file in the same directory, so
// other processes never see a half-written file.
func WriteAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".sync-*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename

	_, err = tmp.Write(data)
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
