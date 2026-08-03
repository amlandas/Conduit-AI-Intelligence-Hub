package kbservice

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/simpleflo/conduit/internal/config"
)

// Path safety for `conduit kb add`.
//
// WP-3.4 background. config.PolicyConfig has carried ForbiddenPaths and
// WarnPaths since v0 -- populated with sensible defaults, expanded at load,
// printed by `conduit config`, and covered by config_test. Nothing ever read
// them. `conduit kb add ~/.ssh` was accepted, and every private key in it was
// chunked into a full-text index that the MCP server hands to any connected AI
// client on request.
//
// internal/policy held a second, hardcoded copy of the same lists inside a
// container-permission Engine whose only consumer was deleted in WP-3.2. Two
// sources of truth, one of them dead, neither enforced. The engine is deleted;
// the config lists -- the ones a user can see and edit -- are enforced here.

// ErrPathForbidden is returned when a source path is inside a directory the
// configuration marks forbidden.
type ErrPathForbidden struct {
	Path      string // the path the user asked to add, as given
	Forbidden string // the configured entry that matched
}

func (e *ErrPathForbidden) Error() string {
	return fmt.Sprintf("refusing to index %s: it is inside %s, which kb.policy.forbidden_paths "+
		"marks as forbidden. Indexing it would copy its contents into a searchable database "+
		"that the MCP server exposes to connected AI clients.", e.Path, e.Forbidden)
}

// checkSourcePath decides whether a directory may become a knowledge base
// source.
//
// It returns an *ErrPathForbidden when the path is inside a forbidden entry,
// and a list of human-readable warnings when it is inside a warn entry.
// Warnings do not block: they exist because ~/Documents is both a perfectly
// normal thing to index and a place people keep tax returns.
func checkSourcePath(cfg *config.Config, path string) ([]string, error) {
	if cfg == nil {
		return nil, nil
	}

	abs := resolvePath(path)
	inTemp := isTempPath(abs)

	// swallowedByTemp reports whether a matching rule should be ignored because
	// the path is scratch space.
	//
	// The exception has to exist: on macOS the per-user temp directory lives
	// under /var, which the shipped defaults forbid, and throwaway data is
	// exactly what a user indexes while trying things out. The deleted policy
	// engine carried the same exception for the same reason.
	//
	// It applies only to BROAD system rules that happen to contain the temp
	// tree. A rule naming something inside temp was written deliberately and is
	// still enforced.
	swallowedByTemp := func(rule string) bool { return inTemp && !isTempPath(rule) }

	for _, forbidden := range cfg.Policy.ForbiddenPaths {
		rule := resolvePath(forbidden)
		if pathIsWithin(abs, rule) && !swallowedByTemp(rule) {
			return nil, &ErrPathForbidden{Path: path, Forbidden: forbidden}
		}
	}

	var warnings []string
	for _, warn := range cfg.Policy.WarnPaths {
		rule := resolvePath(warn)
		if pathIsWithin(abs, rule) && !swallowedByTemp(rule) {
			warnings = append(warnings, fmt.Sprintf(
				"%s is inside %s, which kb.policy.warn_paths flags as sensitive. "+
					"Everything matching your include patterns will become searchable.", path, warn))
		}
	}
	return warnings, nil
}

// resolvePath makes a path absolute and follows symlinks, so that a link
// pointing into ~/.ssh is caught rather than compared lexically.
//
// Both sides of every comparison go through this. Resolving only one would make
// the check wrong on macOS, where /etc is itself a symlink to /private/etc.
//
// A path that cannot be resolved -- it may not exist -- falls back to the
// lexical form. The caller's os.Stat rejects a missing directory shortly
// afterwards anyway.
func resolvePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// isTempPath reports whether path is inside a system temporary directory.
func isTempPath(path string) bool {
	candidates := []string{os.TempDir(), "/tmp", "/var/tmp"}
	if runtime.GOOS == "darwin" {
		candidates = append(candidates, "/var/folders", "/private/var/folders", "/private/tmp")
	}
	for _, c := range candidates {
		if pathIsWithin(path, resolvePath(c)) {
			return true
		}
	}
	return false
}

// pathIsWithin reports whether path is parent itself or sits underneath it.
//
// Comparison is component-wise via the separator, so "/etcetera" is not inside
// "/etc". Windows paths are compared case-insensitively, matching the
// filesystem.
//
// A filesystem root ("/" on POSIX, "C:\" on Windows) matches ONLY itself.
// Everything is underneath the root, so treating it as a prefix would make a
// single "/" entry in forbidden_paths -- which the shipped defaults contain,
// meaning "do not index the whole machine" -- refuse every path there is.
// Both sides are expected to be absolute and symlink-resolved already; see
// resolvePath.
func pathIsWithin(path, parent string) bool {
	if strings.TrimSpace(parent) == "" {
		return false
	}

	path = filepath.Clean(path)
	parent = filepath.Clean(parent)

	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
		parent = strings.ToLower(parent)
	}

	if path == parent {
		return true
	}
	if isFilesystemRoot(parent) {
		return false
	}

	return strings.HasPrefix(path, parent+string(filepath.Separator))
}

// isFilesystemRoot reports whether p is its own parent directory.
func isFilesystemRoot(p string) bool { return filepath.Dir(p) == p }
