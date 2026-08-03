//go:build windows

package embed

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// lockHandle is an exclusive lock backed by O_EXCL file creation.
//
// Windows has no flock equivalent in the standard library, so this falls back
// to a sentinel file. A stale lock left by a crashed process is broken after
// lockStaleAfter so the system cannot wedge permanently.
type lockHandle struct {
	path string
	f    *os.File
}

// lockStaleAfter is how old a lock file must be before it is force-broken.
const lockStaleAfter = 2 * time.Minute

// acquireLock takes an exclusive lock on path, polling until ctx expires.
func acquireLock(ctx context.Context, path string) (*lockHandle, error) {
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err == nil {
			return &lockHandle{path: path, f: f}, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("embed: lock %s: %w", path, err)
		}

		// Break a stale lock rather than deadlocking forever.
		if info, statErr := os.Stat(path); statErr == nil {
			if time.Since(info.ModTime()) > lockStaleAfter {
				_ = os.Remove(path)
				continue
			}
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("embed: timed out waiting for lock %s: %w", path, ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// release removes the lock file.
func (l *lockHandle) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	closeErr := l.f.Close()
	l.f = nil
	rmErr := os.Remove(l.path)
	if closeErr != nil {
		return closeErr
	}
	if rmErr != nil && !os.IsNotExist(rmErr) {
		return rmErr
	}
	return nil
}

// setProcessGroup is a no-op on Windows; process trees are handled by Kill.
func setProcessGroup(cmd *exec.Cmd) {}

// processAlive reports whether pid refers to a live process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Windows FindProcess opens a handle; a released handle means the
	// process has exited.
	defer func() { _ = p.Release() }()
	return true
}

// terminateProcess stops pid, waiting up to grace for it to exit.
func terminateProcess(pid int, grace time.Duration) error {
	if pid <= 0 {
		return nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if err := p.Kill(); err != nil {
		return nil
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil
}
