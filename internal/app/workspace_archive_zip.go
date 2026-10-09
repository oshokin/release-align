package app

import (
	"context"
	"os"
	"strings"
)

// archiveNames remembers ZIP paths so a later entry cannot replace or cover an earlier one.
type archiveNames struct {
	// kinds maps a slash path without a trailing slash to dir, file, or link.
	kinds map[string]string
	// children records a path that already has a descendant entry.
	children map[string]struct{}
}

const (
	// archiveKindDir is a directory entry.
	archiveKindDir = "dir"
	// archiveKindFile is a regular file entry.
	archiveKindFile = "file"
	// archiveKindLink is a symlink entry.
	archiveKindLink = "link"
)

// newArchiveNames starts an empty path set.
func newArchiveNames() *archiveNames {
	return &archiveNames{
		kinds:    map[string]string{},
		children: map[string]struct{}{},
	}
}

// add records one entry. skip is true when a shared directory is already present.
func (n *archiveNames) add(raw string, mode os.FileMode) (bool, error) {
	name, err := n.archiveEntryName(raw)
	if err != nil {
		return false, err
	}

	key := strings.TrimSuffix(name, "/")
	kind := n.archiveEntryKind(name, mode)

	if prev, ok := n.kinds[key]; ok {
		if prev == archiveKindDir && kind == archiveKindDir {
			return true, nil
		}

		return false, errArchiveEntry
	}

	if err = n.parents(key, kind); err != nil {
		return false, err
	}

	n.kinds[key] = kind

	return false, nil
}

// parents rejects a file or symlink that covers another entry, and a file used as a directory.
func (n *archiveNames) parents(key, kind string) error {
	parent := key

	for {
		slash := strings.LastIndex(parent, "/")
		if slash < 0 {
			break
		}

		parent = parent[:slash]

		if prev, ok := n.kinds[parent]; ok && prev != archiveKindDir {
			return errArchiveEntry
		}
	}

	if kind != archiveKindDir {
		if _, ok := n.children[key]; ok {
			return errArchiveEntry
		}
	}

	parent = key

	for {
		slash := strings.LastIndex(parent, "/")
		if slash < 0 {
			break
		}

		parent = parent[:slash]
		n.children[parent] = struct{}{}
	}

	return nil
}

// archiveEntryName rejects absolute paths, backslashes, and parent-directory segments.
func (n *archiveNames) archiveEntryName(raw string) (string, error) {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.Contains(raw, "\\") || strings.Contains(raw, "\x00") {
		return "", errArchiveEntry
	}

	directory := strings.HasSuffix(raw, "/")
	cleaned := n.pathCleanSlash(raw)

	if cleaned == "." || cleaned == pathDotDot || strings.HasPrefix(cleaned, pathDotDot+"/") {
		return "", errArchiveEntry
	}

	if directory {
		cleaned += "/"
	}

	return cleaned, nil
}

// pathCleanSlash cleans a ZIP path without turning it into a host path.
func (n *archiveNames) pathCleanSlash(raw string) string {
	parts := strings.Split(strings.TrimSuffix(raw, "/"), "/")
	kept := make([]string, 0, len(parts))

	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}

		if part == pathDotDot {
			return pathDotDot + "/" + raw
		}

		kept = append(kept, part)
	}

	return strings.Join(kept, "/")
}

// archiveEntryKind classifies a ZIP mode.
func (n *archiveNames) archiveEntryKind(name string, mode os.FileMode) string {
	if strings.HasSuffix(name, "/") || mode.IsDir() {
		return archiveKindDir
	}

	if mode&os.ModeSymlink != 0 {
		return archiveKindLink
	}

	return archiveKindFile
}

// allowedRepoEntry reports that name is the project, a path inside it, or a parent directory.
func allowedRepoEntry(name, project string) bool {
	key := strings.TrimSuffix(name, "/")
	if key == project || strings.HasPrefix(name, project+"/") {
		return true
	}

	return strings.HasPrefix(project, key+"/")
}

// archiveCanceled reports that the caller stopped the run.
func archiveCanceled(ctx context.Context) error {
	if ctx == nil {
		return nil
	}

	return ctx.Err()
}
