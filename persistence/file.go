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

// FilePersistence keeps the state of a room in the file dir/room, so that
// other programs can read and edit it. It implements Watcher.
type FilePersistence struct {
	dir string
}

// NewFilePersistence opens (creating if necessary) a store in the directory dir.
func NewFilePersistence(dir string) (*FilePersistence, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create %s: %w", dir, err)
	}
	return &FilePersistence{dir: filepath.Clean(dir)}, nil
}

// path returns the file of room. A room is a plain file name: anything that
// could leave dir is refused.
func (f *FilePersistence) path(room string) (string, error) {
	if room == "" || room == "." || room == ".." || filepath.Base(room) != room {
		return "", fmt.Errorf("room %q must be a plain file name", room)
	}
	return filepath.Join(f.dir, room), nil
}

func (f *FilePersistence) Load(ctx context.Context, room string) ([]byte, error) {
	p, err := f.path(room)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrRoomNotFound, p)
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", p, err)
	}
	return data, nil
}

func (f *FilePersistence) Store(ctx context.Context, room string, state []byte) error {
	p, err := f.path(room)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return WriteAtomic(p, state)
}

// Watch reports every event on the file of room. The directory is watched,
// not the file: editors that save by renaming and WriteAtomic replace the
// file, which ends a file watch.
func (f *FilePersistence) Watch(ctx context.Context, room string) (<-chan struct{}, error) {
	p, err := f.path(room)
	if err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch %s: %w", p, err)
	}
	if err := w.Add(f.dir); err != nil {
		w.Close()
		return nil, fmt.Errorf("watch %s: %w", f.dir, err)
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
