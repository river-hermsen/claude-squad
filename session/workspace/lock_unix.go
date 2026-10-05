//go:build !windows

package workspace

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes an exclusive lock on path, creating it if needed, and returns the unlock
// function. It blocks until the lock is free.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN)
		f.Close()
	}, nil
}
