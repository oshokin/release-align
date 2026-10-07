package app

import (
	"encoding/json"
	"fmt"
	"io"
)

// FormatArchiveReport writes the text summary.
func FormatArchiveReport(w io.Writer, report *ArchiveReport) error {
	if w == nil || report == nil {
		return nil
	}

	if report.Error != "" {
		_, err := fmt.Fprintf(w, "%s\nNo archive file was published.\n", report.Error)

		return err
	}

	_, err := fmt.Fprintf(
		w,
		"%s\nArchived: %d\nFile: %s\nGitlinks: %d\n",
		report.Source,
		report.Repositories,
		report.File,
		report.Gitlinks,
	)
	if err != nil {
		return err
	}

	if report.Gitlinks == 0 {
		return nil
	}

	_, err = fmt.Fprintln(
		w,
		"Gitlinks are recorded in the manifest. Submodule contents are not in the archive.",
	)

	return err
}

// WriteArchiveReport writes one JSON document.
func WriteArchiveReport(w io.Writer, report *ArchiveReport) error {
	if report == nil {
		return errWorkspaceNilReport
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	return enc.Encode(report)
}
