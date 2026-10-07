package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
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
