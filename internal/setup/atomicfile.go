package setup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeFileAtomic replaces a file's contents without ever leaving it truncated.
//
// os.WriteFile opens with O_TRUNC: the old contents are gone the instant the
// call starts, and a crash, a full disk or a killed process between that moment
// and the final byte leaves an empty or half-written file. That is an
// acceptable risk for a cache. It is not acceptable for ~/.claude.json, which
// holds every MCP server the user has configured plus Claude Code's own state,
// nor for a shell profile, where a truncated file means the next login shell
// comes up broken.
//
// Writing a sibling temp file and renaming it makes the replacement atomic: any
// reader sees either the old file or the new one, never a partial one. The temp
// file lives in the destination directory because rename is only atomic within
// a filesystem.
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	// CreateTemp makes the file 0600. Restoring the intended mode before the
	// rename means the file is never visible at the destination with the wrong
	// permissions -- which for a shell profile would silently change who can
	// read it.
	if err := tmp.Chmod(mode); err != nil {
		return fmt.Errorf("chmod %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	tmpName = ""
	return nil
}

// backupSuffix is appended to a file's name when Conduit copies it aside.
const backupSuffix = ".conduit-uninstall.bak"

// backupFile copies path to a sibling backup and returns the backup's name.
//
// Atomic replacement guarantees the file is never corrupt; it does not
// guarantee the new contents are what the user wanted. For a shell profile that
// distinction matters, because the failure mode of a wrong edit is a login
// shell that no longer works, and the user's recourse has to be something
// better than "restore it from memory".
func backupFile(path string) (string, error) {
	src, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = src.Close() }()

	mode := os.FileMode(0o644)
	if info, serr := src.Stat(); serr == nil {
		mode = info.Mode().Perm()
	}

	backup := path + backupSuffix
	dst, err := os.OpenFile(backup, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return "", fmt.Errorf("create %s: %w", backup, err)
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		return "", fmt.Errorf("copy to %s: %w", backup, err)
	}
	if err := dst.Close(); err != nil {
		return "", fmt.Errorf("close %s: %w", backup, err)
	}
	return backup, nil
}
