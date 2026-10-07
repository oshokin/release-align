package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// workspaceDocument is one regular workspace file read for a refresh.
type workspaceDocument struct {
	// path is the workspace file path.
	path string
	// info is the file metadata captured before publication.
	info os.FileInfo
	// data is the raw file body.
	data []byte
	// spec is the decoded inventory.
	spec *WorkspaceSpec
}

// workspacePublishHooks replaces steps of one publication. Nil hooks use the real filesystem.
// A close hook must close the file. Tests use this to inject short writes and rename failures.
type workspacePublishHooks struct {
	// write replaces the write of the temporary file.
	write func(*os.File, []byte) error
	// sync replaces the fsync of the temporary file.
	sync func(*os.File) error
	// close replaces closing the temporary file.
	close func(*os.File) error
	// rename replaces the final rename onto the destination.
	rename func(oldName, newName string) error
	// beforeRename runs after the temporary file is closed and before rename.
	beforeRename func(path string) error
}

var (
	// errWorkspaceFileKind means the path is not a regular file.
	errWorkspaceFileKind = errors.New("workspace file must be a regular file, not a symbolic link")
	// errWorkspaceFileChanged means the file changed while it was locked.
	errWorkspaceFileChanged = errors.New("workspace file changed before publication")
	// errWorkspaceRefreshLock means another writer already holds the lock.
	errWorkspaceRefreshLock = errors.New(
		"workspace refresh lock is held; another refresh may be running, or a stale lock remains",
	)
)

// CreateWorkspaceFile writes a new validated inventory and refuses every existing destination.
// The document is checked against the existing 1 MiB decoder before the destination is opened.
// It does not merge inventories or replace files, and does not promise crash-atomic publication.
func CreateWorkspaceFile(filename string, spec *WorkspaceSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return err
	}

	data = append(data, '\n')
	if _, err = DecodeWorkspace(bytes.NewReader(data)); err != nil {
		return err
	}

	file, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}

	return finishExclusive(file, filename, data)
}

// canonicalWorkspaceFilename resolves the parent directory and keeps the final name unfollowed.
func canonicalWorkspaceFilename(filename string) (string, error) {
	absolute, err := filepath.Abs(filename)
	if err != nil {
		return "", err
	}

	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}

	return filepath.Join(parent, filepath.Base(absolute)), nil
}

// lockWorkspaceFile creates an exclusive sibling lock and returns a function that removes only that lock.
func lockWorkspaceFile(path string) (func(), error) {
	lockPath := path + ".lock"

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("%w: %s: %w", errWorkspaceRefreshLock, lockPath, err)
		}

		return nil, fmt.Errorf("create workspace lock %s: %w", lockPath, err)
	}

	_, writeErr := fmt.Fprintf(file, "pid=%d\n", os.Getpid())
	closeErr := file.Close()

	if writeErr != nil || closeErr != nil {
		_ = os.Remove(lockPath)

		return nil, errors.Join(writeErr, closeErr)
	}

	return func() {
		_ = os.Remove(lockPath)
	}, nil
}

// readWorkspaceDocument loads one regular file and decodes it with the existing size limit.
func readWorkspaceDocument(path string) (*workspaceDocument, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}

	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errWorkspaceFileKind
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, workspaceMaxBytes+1))
	if err != nil {
		return nil, err
	}

	spec, err := DecodeWorkspace(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	document := &workspaceDocument{
		path: path,
		info: info,
		data: data,
		spec: spec,
	}

	return document, nil
}

// publishWorkspace replaces a regular file via a temporary sibling and rename.
// The lock, when held by the caller, stays held until this function returns.
// On Unix, rename replaces the directory entry. This is not a crash-proof transaction on every OS.
func publishWorkspace(
	ctx context.Context,
	document *workspaceDocument,
	spec *WorkspaceSpec,
	hooks *workspacePublishHooks,
) error {
	data, err := encodeWorkspace(spec)
	if err != nil {
		return err
	}

	temp, err := os.CreateTemp(filepath.Dir(document.path), ".release-align-*")
	if err != nil {
		return err
	}

	tempName := temp.Name()
	removeTemp := true

	defer func() {
		if removeTemp {
			_ = os.Remove(tempName)
		}
	}()

	if err = preparePublishedFile(temp, document.info, data, hooks); err != nil {
		_ = temp.Close()

		return err
	}

	if err = closePublishedFile(temp, hooks); err != nil {
		return err
	}

	if err = readyToPublish(ctx, document, hooks); err != nil {
		return err
	}

	if err = renamePublishedFile(tempName, document.path, hooks); err != nil {
		return err
	}

	removeTemp = false

	return nil
}

// encodeWorkspace checks the replacement against the same decoder used for init.
func encodeWorkspace(spec *WorkspaceSpec) ([]byte, error) {
	if err := spec.Validate(); err != nil {
		return nil, err
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		return nil, err
	}

	data = append(data, '\n')
	if _, err = DecodeWorkspace(bytes.NewReader(data)); err != nil {
		return nil, err
	}

	return data, nil
}

// preparePublishedFile copies the mode, writes the document, and syncs it before publication.
func preparePublishedFile(file *os.File, info os.FileInfo, data []byte, hooks *workspacePublishHooks) error {
	if err := file.Chmod(info.Mode().Perm()); err != nil {
		return err
	}

	if err := writePublishedFile(file, data, hooks); err != nil {
		return err
	}

	return syncPublishedFile(file, hooks)
}

// writePublishedFile rejects a short write before the destination name is replaced.
func writePublishedFile(file *os.File, data []byte, hooks *workspacePublishHooks) error {
	if hooks != nil && hooks.write != nil {
		return hooks.write(file, data)
	}

	n, err := file.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}

	return err
}

// syncPublishedFile pushes the temporary file to the filesystem before it is renamed.
func syncPublishedFile(file *os.File, hooks *workspacePublishHooks) error {
	if hooks != nil && hooks.sync != nil {
		return hooks.sync(file)
	}

	return file.Sync()
}

// closePublishedFile closes the temporary file. A test hook must close it as well.
func closePublishedFile(file *os.File, hooks *workspacePublishHooks) error {
	if hooks != nil && hooks.close != nil {
		return hooks.close(file)
	}

	return file.Close()
}

// readyToPublish refuses the rename when the caller is canceled or the file was edited.
func readyToPublish(ctx context.Context, document *workspaceDocument, hooks *workspacePublishHooks) error {
	if hooks != nil && hooks.beforeRename != nil {
		if err := hooks.beforeRename(document.path); err != nil {
			return err
		}
	}

	if err := ctx.Err(); err != nil {
		return context.Cause(ctx)
	}

	return sameWorkspaceBytes(document)
}

// renamePublishedFile publishes the temporary name onto the workspace path.
func renamePublishedFile(oldName, newName string, hooks *workspacePublishHooks) error {
	if hooks != nil && hooks.rename != nil {
		return hooks.rename(oldName, newName)
	}

	return os.Rename(oldName, newName)
}

// sameWorkspaceBytes refuses publication when the inode or the bytes no longer match the read.
func sameWorkspaceBytes(document *workspaceDocument) error {
	info, err := os.Lstat(document.path)
	if err != nil {
		return err
	}

	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || !os.SameFile(document.info, info) {
		return errWorkspaceFileChanged
	}

	file, err := os.Open(document.path)
	if err != nil {
		return err
	}

	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, workspaceMaxBytes+1))
	if err != nil {
		return err
	}

	if !bytes.Equal(data, document.data) {
		return errWorkspaceFileChanged
	}

	return nil
}

// finishExclusive writes one new file and deletes it when write or close fails.
// A destination that OpenFile refused is left untouched.
func finishExclusive(file io.WriteCloser, filename string, data []byte) error {
	n, writeErr := file.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}

	closeErr := file.Close()
	if writeErr == nil && closeErr == nil {
		return nil
	}

	return errors.Join(writeErr, closeErr, os.Remove(filename))
}
