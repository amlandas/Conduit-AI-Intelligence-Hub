//go:build !windows

package embed

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// lockHandle is an advisory exclusive lock held on an open file descriptor.
//
// flock is used rather than an O_EXCL sentinel file because the kernel releases
// it automatically when the holding process dies. That matters here: a conduit
// process killed mid-spawn must not wedge every other process forever.
type lockHandle struct {
	f *os.File
}

// acquireLock takes an exclusive advisory lock on path, creating it if needed.
// It polls until the lock is granted or ctx expires.
func acquireLock(ctx context.Context, path string) (*lockHandle, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("embed: open lock file %s: %w", path, err)
	}

	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &lockHandle{f: f}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = f.Close()
			return nil, fmt.Errorf("embed: lock %s: %w", path, err)
		}

		select {
		case <-ctx.Done():
			_ = f.Close()
			return nil, fmt.Errorf("embed: timed out waiting for lock %s: %w", path, ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// release drops the lock and closes the descriptor.
func (l *lockHandle) release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	closeErr := l.f.Close()
	l.f = nil
	if err != nil {
		return err
	}
	return closeErr
}

// setProcessGroup puts the child in its own process group so the entire tree
// can be signalled at once. Without this, killing llama-server could leave
// grandchildren orphaned.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// processAlive reports whether pid refers to a live process.
//
// Signal 0 performs permission and existence checks without delivering a
// signal. EPERM means the process exists but is owned by another user.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}

// terminateProcess stops pid's process group, escalating from SIGTERM to
// SIGKILL after grace. It returns nil once the process is confirmed gone.
func terminateProcess(pid int, grace time.Duration) error {
	if pid <= 0 || !processAlive(pid) {
		return nil
	}

	// Negative pid targets the whole process group. Fall back to the bare pid
	// if the process was not group leader (e.g. state written by an older
	// version that did not set Setpgid).
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		_ = syscall.Kill(pid, syscall.SIGTERM)
	}

	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}

	if processAlive(pid) {
		return fmt.Errorf("embed: process %d survived SIGKILL", pid)
	}
	return nil
}
