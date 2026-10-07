package app

import (
	"fmt"
	"os"
	"path/filepath"
)

// lockBase acquires one directory lock for the base directory.
// Git still owns its normal per-operation locks.
func lockBase(base string) (func(), error) {
	path := filepath.Join(base, ".release-align.lock")
	if err := os.Mkdir(path, 0o700); err != nil {
		return nil, fmt.Errorf("cannot acquire %s (another run or a stale lock): %w", path, err)
	}

	if err := os.WriteFile(
		filepath.Join(path, "owner"),
		fmt.Appendf(nil, "pid=%d\n", os.Getpid()),
		0o600,
	); err != nil {
		_ = os.Remove(path)

		return nil, err
	}

	return func() {
		_ = os.Remove(filepath.Join(path, "owner"))
		_ = os.Remove(path)
	}, nil
}
