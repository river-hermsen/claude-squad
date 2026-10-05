//go:build windows

package workspace

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on path, creating it if needed, and returns the unlock
// function. It blocks until the lock is free.
func lockFile(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}
	h := windows.Handle(f.Fd())
	if err := windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, new(windows.Overlapped)); err != nil {
		f.Close()
		return nil, err
	}
	return func() {
		_ = windows.UnlockFileEx(h, 0, 1, 0, new(windows.Overlapped))
		f.Close()
	}, nil
}
