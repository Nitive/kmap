package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var ErrDaemonNotRunning = errors.New("kmap daemon is not running")

func ControlPIDPath() string {
	if runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); runtimeDir != "" {
		return filepath.Join(runtimeDir, "kmap.pid")
	}
	return filepath.Join(os.TempDir(), "kmap.pid")
}

func writePIDFile() (func(), error) {
	path := ControlPIDPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}

	pid := strconv.Itoa(os.Getpid())
	if err := os.WriteFile(path, []byte(pid+"\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write pid file %s: %w", path, err)
	}

	cleanup := func() {
		raw, err := os.ReadFile(path)
		if err != nil {
			return
		}
		if strings.TrimSpace(string(raw)) == pid {
			_ = os.Remove(path)
		}
	}
	return cleanup, nil
}

func SignalGrabOverride(release bool) error {
	raw, err := os.ReadFile(ControlPIDPath())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrDaemonNotRunning
		}
		return fmt.Errorf("read daemon pid file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return fmt.Errorf("parse daemon pid: %w", err)
	}

	sig := syscall.SIGUSR2
	if release {
		sig = syscall.SIGUSR1
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find daemon process %d: %w", pid, err)
	}
	if err := proc.Signal(sig); err != nil {
		return fmt.Errorf("signal daemon process %d: %w", pid, err)
	}
	return nil
}
