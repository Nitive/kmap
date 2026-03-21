package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

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
