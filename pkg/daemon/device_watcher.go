package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

type deviceWatcher interface {
	Events() <-chan struct{}
	Errors() <-chan error
	Sync(paths []string) error
	Close() error
}

type inotifyDeviceWatcher struct {
	file    *os.File
	events  chan struct{}
	errors  chan error
	mask    uint32
	mu      sync.Mutex
	wdByDir map[string]int
	dirByWD map[int]string
}

const deviceWatchMask = syscall.IN_CREATE |
	syscall.IN_MOVED_TO |
	syscall.IN_MOVED_FROM |
	syscall.IN_DELETE |
	syscall.IN_DELETE_SELF |
	syscall.IN_ATTRIB

func newDeviceWatcher() (deviceWatcher, error) {
	fd, err := syscall.InotifyInit1(syscall.IN_CLOEXEC)
	if err != nil {
		return nil, fmt.Errorf("init inotify: %w", err)
	}

	w := &inotifyDeviceWatcher{
		file:    os.NewFile(uintptr(fd), "kmap-device-watcher"),
		events:  make(chan struct{}, 1),
		errors:  make(chan error, 1),
		mask:    deviceWatchMask,
		wdByDir: make(map[string]int),
		dirByWD: make(map[int]string),
	}
	go w.readLoop()
	return w, nil
}

func (w *inotifyDeviceWatcher) Events() <-chan struct{} {
	return w.events
}

func (w *inotifyDeviceWatcher) Errors() <-chan error {
	return w.errors
}

func (w *inotifyDeviceWatcher) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *inotifyDeviceWatcher) Sync(paths []string) error {
	if w == nil || w.file == nil {
		return nil
	}

	targetDirs := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if dir, ok := watchDirForPath(path); ok {
			targetDirs[dir] = struct{}{}
		}
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	fd := int(w.file.Fd())

	for dir, wd := range w.wdByDir {
		if _, keep := targetDirs[dir]; keep {
			continue
		}
		delete(w.wdByDir, dir)
		delete(w.dirByWD, wd)
		if _, err := syscall.InotifyRmWatch(fd, uint32(wd)); err != nil &&
			!errors.Is(err, syscall.EINVAL) &&
			!errors.Is(err, syscall.EBADF) {
			return fmt.Errorf("remove watch %s: %w", dir, err)
		}
	}

	for dir := range targetDirs {
		if _, exists := w.wdByDir[dir]; exists {
			continue
		}
		wd, err := syscall.InotifyAddWatch(fd, dir, w.mask)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) || errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENOTDIR) {
				continue
			}
			return fmt.Errorf("watch %s: %w", dir, err)
		}
		w.wdByDir[dir] = wd
		w.dirByWD[wd] = dir
	}

	return nil
}

func (w *inotifyDeviceWatcher) readLoop() {
	defer close(w.events)
	defer close(w.errors)

	buf := make([]byte, 4096)
	for {
		n, err := w.file.Read(buf)
		if err != nil {
			if errors.Is(err, os.ErrClosed) || errors.Is(err, syscall.EBADF) {
				return
			}
			w.sendError(fmt.Errorf("read inotify events: %w", err))
			return
		}
		if n == 0 {
			continue
		}
		w.sendEvent()
	}
}

func (w *inotifyDeviceWatcher) sendEvent() {
	select {
	case w.events <- struct{}{}:
	default:
	}
}

func (w *inotifyDeviceWatcher) sendError(err error) {
	select {
	case w.errors <- err:
	default:
	}
}

func watchDirForPath(path string) (string, bool) {
	dir := filepath.Clean(filepath.Dir(path))
	for {
		info, err := os.Stat(dir)
		if err == nil && info.IsDir() {
			return dir, true
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", false
}
