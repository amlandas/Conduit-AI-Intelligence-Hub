package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Path-safety errors. Callers match on these rather than on message text.
var (
	// ErrUnsafeDataDir means a path was rejected as a deletion target because
	// it names something no Conduit installation could plausibly own.
	ErrUnsafeDataDir = errors.New("setup: unsafe data directory")

	// ErrSymlinkedDataDir means the data directory is a symbolic link.
	ErrSymlinkedDataDir = errors.New("setup: data directory is a symlink")
)

// CanonicalDataDir resolves a user-supplied data directory to the exact path a
// removal would act on.
//
// This has to happen before any safety check, because every interesting evasion
// is a spelling difference rather than a different path: "~/", "//", "/.",
// "/Users/", and "x/.." all name directories that a naive string comparison
// against a deny list waves straight through. Shells hand us trailing slashes
// routinely -- tab completion appends one -- so "$HOME/" reaching a guard that
// only knows about "$HOME" is the normal case, not a contrived one.
//
// The trailing separator matters for a second reason. On macOS, `rm -rf
// symlink/` deletes the contents of the link's *target*, while `rm -rf symlink`
// removes only the link. Canonicalising strips the separator so the two spellings
// cannot mean different things.
func CanonicalDataDir(dir string) (string, error) {
	if strings.TrimSpace(dir) == "" {
		return "", fmt.Errorf("%w: path is empty", ErrUnsafeDataDir)
	}

	expanded := expandHome(dir)

	// Abs makes the path absolute against the working directory and calls
	// Clean, which resolves "." and "..", collapses repeated separators, and
	// drops any trailing separator except on root itself.
	abs, err := filepath.Abs(expanded)
	if err != nil {
		return "", fmt.Errorf("%w: cannot resolve %q: %v", ErrUnsafeDataDir, dir, err)
	}

	return filepath.Clean(abs), nil
}

// expandHome expands a leading ~ to the user's home directory.
//
// Go does no tilde expansion, so a quoted "~/conduit" that the shell passed
// through literally would otherwise be canonicalised into a directory called
// "~" under the working directory -- a path that looks safe to a deny list and
// is not what the user meant either way.
func expandHome(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, strings.TrimPrefix(path, "~/"))
}

// protectedDirs returns the canonical paths that must never be a data directory.
//
// The mount points are here because an external disk, a network share or a
// container bind mount is somebody's whole filesystem, and `--data-dir
// /Volumes` differs from a real one by a single missing path component.
//
// "/System/Volumes/Data" is listed separately from "/System" because this list
// is matched by exact path and by device:inode, and neither of those catches a
// CHILD of a protected directory. On macOS Catalina and later the system volume
// is read-only and everything writable -- every user's home included -- lives
// on a second volume mounted there. It is its own mount with its own inode,
// distinct from both "/" and "/Users", so nothing already here resembled it.
func protectedDirs() []string {
	dirs := []string{
		"/", "/usr", "/etc", "/var", "/opt", "/tmp", "/bin", "/sbin",
		"/home", "/Users", "/root", "/System", "/System/Volumes/Data",
		"/Library", "/Applications",
		"/private", "/dev", "/proc", "/boot",
		"/Volumes", "/mnt", "/media", "/net", "/srv",
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		clean := filepath.Clean(home)
		dirs = append(dirs, clean)
		// The directory holding every user's home (/Users, /home) is already
		// listed, but a non-standard home such as /export/people/amlan has a
		// parent worth protecting too.
		if parent := filepath.Dir(clean); parent != clean && parent != "/" {
			dirs = append(dirs, parent)
		}
	}
	return dirs
}

// sameDir reports whether two paths name the same directory on disk.
//
// String comparison is not enough, and on macOS it is not even close. APFS is
// case-insensitive by default, so "/USERS/amlan" opens exactly the same
// directory as "/Users/amlan" while comparing unequal to every entry in the
// deny list. The same hole swallows Unicode: a home directory containing an
// accented character can be spelled in NFC or NFD, both of which the filesystem
// accepts and neither of which matches the other byte for byte.
//
// os.SameFile compares device and inode, which is what the kernel itself uses
// to decide whether two names are one file. It cannot be spelled around.
func sameDir(a, b string) bool {
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// AssertSafeDataDir rejects a path that must never be handed to a recursive
// delete, and returns the canonical form of one that is acceptable.
//
// It is deliberately implemented here rather than only in the shell scripts.
// The scripts are one caller among several -- the binary is invoked directly by
// users, by the desktop GUI and by other scripts -- and a guard that only exists
// in the wrapper protects nobody who skips the wrapper.
func AssertSafeDataDir(dir string) (string, error) {
	canonical, err := CanonicalDataDir(dir)
	if err != nil {
		return "", err
	}

	for _, protected := range protectedDirs() {
		// Lexical first: it is the only check available for a protected path
		// that does not exist on this machine (/mnt on a Mac, /Volumes on
		// Linux), and it costs nothing.
		if canonical == protected {
			return "", fmt.Errorf("%w: refusing to treat %s (resolved from %q) as a Conduit data directory",
				ErrUnsafeDataDir, canonical, dir)
		}
		// Then by identity, which catches every spelling the filesystem
		// accepts and the string comparison does not.
		if sameDir(canonical, protected) {
			return "", fmt.Errorf("%w: refusing to treat %s as a Conduit data directory (it is %s under a different spelling)",
				ErrUnsafeDataDir, canonical, protected)
		}
	}

	return canonical, nil
}

// AssertRemovableDataDir is AssertSafeDataDir plus a symlink check.
//
// A symlinked data directory is refused rather than followed. The two obvious
// behaviours disagree with each other in a way that loses data either way:
// os.RemoveAll deletes the link and leaves the real directory behind, so the
// user believes their knowledge base is gone when it is not, while `rm -rf
// dir/` on macOS empties the target and leaves the link, which is the same
// mistake in the opposite direction. Refusing keeps the script and the binary
// identical and puts the choice back where it belongs.
//
// A path that does not exist is not an error here: there is nothing to remove,
// and the caller reports that.
func AssertRemovableDataDir(dir string) (string, error) {
	canonical, err := AssertSafeDataDir(dir)
	if err != nil {
		return "", err
	}

	info, err := os.Lstat(canonical)
	if err != nil {
		if os.IsNotExist(err) {
			return canonical, nil
		}
		return "", fmt.Errorf("%w: cannot inspect %s: %v", ErrUnsafeDataDir, canonical, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		target, rerr := os.Readlink(canonical)
		if rerr != nil {
			target = "unreadable"
		}
		return "", fmt.Errorf("%w: %s points at %s.\nRemoving it would either delete the link and keep your data, or delete the data and keep the link, depending on the tool.\nRe-run against the resolved path if you mean to delete the data",
			ErrSymlinkedDataDir, canonical, target)
	}

	return canonical, nil
}

// ErrNotConduitDataDir means a deletion target holds none of Conduit's files.
var ErrNotConduitDataDir = errors.New("setup: not a Conduit data directory")

// looksLikeConduitDataDir reports whether a directory holds the files a real
// Conduit installation would have put there.
//
// It is the difference between "delete ~/.conduit" and "delete whatever this
// flag happened to point at", and it is the last check standing between a
// mistyped --data-dir and a recursive delete of a directory full of somebody
// else's files.
func looksLikeConduitDataDir(dir string) bool {
	for _, marker := range []string{"conduit.db", "conduit.yaml"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

// assertConduitDataDir refuses to delete a directory that is not Conduit's.
//
// Force is the deliberate override, because there are legitimate reasons to
// remove a data directory whose contents are already gone. Everything else --
// a typo in --data-dir, a stale path in a config file, a script that built the
// wrong string -- lands here and stops.
//
// This lives in the library rather than only in the CLI so that the desktop
// GUI, a shell wrapper and anything else linking the package are all covered.
func assertConduitDataDir(dir string, force bool) error {
	if force {
		return nil
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		// Nothing there. Removal is a no-op, so there is nothing to protect.
		return nil
	}
	if looksLikeConduitDataDir(dir) {
		return nil
	}
	return fmt.Errorf("%w: %s contains no conduit.db or conduit.yaml.\nRefusing to delete it recursively. Check the path, or pass Force if you are certain",
		ErrNotConduitDataDir, dir)
}

// DataDirSummary describes a data directory for a confirmation prompt.
type DataDirSummary struct {
	// Path is the canonical directory.
	Path string

	// Exists is false when there is nothing there.
	Exists bool

	// LooksLikeConduit is true when Conduit's own files are present.
	LooksLikeConduit bool

	// SizeBytes is the recursive size, 0 when absent.
	SizeBytes int64

	// Size is SizeBytes rendered for humans.
	Size string
}

// SummarizeDataDir gathers what a user needs to see before confirming a delete.
//
// A prompt that says "this will permanently delete all Conduit data" without
// naming the directory is not a confirmation, because the one fact the user
// needs in order to spot a mistake -- which directory -- is the fact it omits.
func SummarizeDataDir(dir string) DataDirSummary {
	s := DataDirSummary{Path: dir}

	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return s
	}

	s.Exists = true
	s.LooksLikeConduit = looksLikeConduitDataDir(dir)
	s.SizeBytes = dirSize(dir)
	s.Size = humanBytes(s.SizeBytes)
	return s
}
