package app

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// write builds the ZIP in a temporary file and publishes it with a hard link.
func (j *archiveJob) write(planned []*plannedRepo) error {
	if err := archiveCanceled(j.ctx); err != nil {
		return err
	}

	destination := j.destination

	parent := filepath.Dir(destination)

	temp, err := os.CreateTemp(parent, ".release-align-archive-*.zip")
	if err != nil {
		return archivePhase(destination, "prepare", err)
	}

	tempName := temp.Name()
	published := false

	defer func() {
		if !published {
			_ = os.Remove(tempName)
		}
	}()

	if err = j.fill(temp, planned); err != nil {
		_ = temp.Close()

		return err
	}

	if err = temp.Close(); err != nil {
		return archivePhase(destination, "close", err)
	}

	if err = archiveCanceled(j.ctx); err != nil {
		return err
	}

	if err = os.Link(tempName, destination); err != nil {
		return archivePhase(destination, "publish", err)
	}

	published = true

	j.noteTemp(tempName)

	return nil
}

// fill writes repository trees and the manifest. Close is checked before publish.
func (j *archiveJob) fill(temp *os.File, planned []*plannedRepo) error {
	writer := zip.NewWriter(temp)
	names := newArchiveNames()

	for _, repo := range planned {
		if err := j.packRepo(writer, names, repo); err != nil {
			_ = writer.Close()

			return err
		}
	}

	if err := writeArchiveManifest(writer, names, j.manifest(planned)); err != nil {
		_ = writer.Close()

		return err
	}

	if err := writer.Close(); err != nil {
		return archivePhase(j.opts.File, "close", err)
	}

	if err := temp.Sync(); err != nil {
		return archivePhase(j.opts.File, "close", err)
	}

	return nil
}

// packRepo exports one commit and copies its ZIP entries without recompressing them.
func (j *archiveJob) packRepo(writer *zip.Writer, names *archiveNames, repo *plannedRepo) error {
	if err := archiveCanceled(j.ctx); err != nil {
		return err
	}

	j.progressf("%s %s\n", repo.Path, repo.Commit)

	deadline := time.Now().Add(j.opts.ArchiveTimeout)

	repoCtx, cancel := context.WithDeadline(j.ctx, deadline)
	defer cancel()

	tempName, err := repoZipPath(filepath.Dir(j.destination))
	if err != nil {
		return archivePhase(repo.Path, "archive", err)
	}

	defer func() {
		_ = os.Remove(tempName)
	}()

	remain := time.Until(deadline)
	if remain <= 0 {
		return archivePhase(repo.Path, "archive", context.DeadlineExceeded)
	}

	err = j.git.Archive(repoCtx, repo.Dir, repo.Path+"/", tempName, repo.Commit, remain)
	if err != nil {
		return archivePhase(repo.Path, "archive", err)
	}

	return copyRepoZip(repoCtx, writer, names, repo.Path, tempName)
}

// repoZipPath reserves an empty file in dir. git archive overwrites that absolute path.
func repoZipPath(dir string) (string, error) {
	temp, err := os.CreateTemp(dir, ".release-align-repo-*.zip")
	if err != nil {
		return "", err
	}

	name := temp.Name()
	if err = temp.Close(); err != nil {
		_ = os.Remove(name)

		return "", err
	}

	return name, nil
}

// copyRepoZip copies one repository archive into the shared writer.
func copyRepoZip(ctx context.Context, writer *zip.Writer, names *archiveNames, project, path string) error {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return archivePhase(project, "copy", err)
	}

	defer reader.Close()

	for _, file := range reader.File {
		if err = archiveCanceled(ctx); err != nil {
			return err
		}

		if err = copyRepoEntry(ctx, writer, names, project, file); err != nil {
			return archivePhase(project, "copy", err)
		}
	}

	if err = archiveCanceled(ctx); err != nil {
		return archivePhase(project, "copy", err)
	}

	return nil
}

// copyRepoEntry validates one path and copies its stored bytes in bounded chunks.
func copyRepoEntry(
	ctx context.Context,
	writer *zip.Writer,
	names *archiveNames,
	project string,
	file *zip.File,
) error {
	if err := archiveCanceled(ctx); err != nil {
		return err
	}

	if !allowedRepoEntry(file.Name, project) {
		return errArchiveEntry
	}

	skip, err := names.add(file.Name, file.Mode())
	if err != nil || skip {
		return err
	}

	raw, err := file.OpenRaw()
	if err != nil {
		return err
	}

	header := file.FileHeader

	dst, err := writer.CreateRaw(&header)
	if err != nil {
		return err
	}

	return copyBounded(ctx, dst, raw)
}

// copyBounded copies raw ZIP bytes and returns when the context ends between chunks.
func copyBounded(ctx context.Context, dst io.Writer, src io.Reader) error {
	buf := make([]byte, archiveCopyChunk)

	for {
		if err := archiveCanceled(ctx); err != nil {
			return err
		}

		n, err := src.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return writeErr
			}

			if cancelErr := archiveCanceled(ctx); cancelErr != nil {
				return cancelErr
			}
		}

		if errors.Is(err, io.EOF) {
			return archiveCanceled(ctx)
		}

		if err != nil {
			return err
		}
	}
}

// manifest builds the document stored beside the sources.
func (j *archiveJob) manifest(planned []*plannedRepo) *archiveManifest {
	repos := make([]*archiveManifestRepo, 0, len(planned))

	for _, repo := range planned {
		repos = append(repos, &archiveManifestRepo{
			Path:      repo.Path,
			Requested: repo.Requested,
			Commit:    repo.Commit,
			Gitlinks:  repo.Gitlinks,
		})
	}

	return &archiveManifest{
		SchemaVersion: archiveSchemaVersion,
		Release:       j.spec.Release,
		Repositories:  repos,
	}
}

// writeArchiveManifest adds _release-align/manifest.json.
func writeArchiveManifest(writer *zip.Writer, names *archiveNames, manifest *archiveManifest) error {
	name := archiveManifestDir + "/manifest.json"

	if _, err := names.add(name, 0o600); err != nil {
		return archivePhase(name, "manifest", err)
	}

	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	header.SetMode(0o600)

	body, err := writer.CreateHeader(header)
	if err != nil {
		return archivePhase(name, "manifest", err)
	}

	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return archivePhase(name, "manifest", err)
	}

	encoded = append(encoded, '\n')

	if _, err = body.Write(encoded); err != nil {
		return archivePhase(name, "manifest", err)
	}

	return nil
}

// progressf writes one status line to stderr when a writer was provided.
func (j *archiveJob) progressf(format string, args ...any) {
	if j.opts.Progress == nil {
		return
	}

	_, _ = fmt.Fprintf(j.opts.Progress, format, args...)
}

// noteTemp removes the temporary ZIP after a successful publish.
// A cleanup failure does not hide the published file.
func (j *archiveJob) noteTemp(name string) {
	if err := os.Remove(name); err != nil && j.opts.Progress != nil {
		_, _ = fmt.Fprintf(j.opts.Progress, "temporary archive remains at %s: %v\n", name, err)
	}
}
