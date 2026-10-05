package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "embed"
)

type (
	// Release is the tag and commit that identify one service version.
	Release struct {
		// Tag is the version tag.
		Tag string
		// Commit is the expected commit hash.
		Commit string
	}
	Manifest map[string]*Release
)

//go:embed defaults.json
var defaults []byte

var commitPattern = regexp.MustCompile(`^[a-fA-F0-9]{4,64}$`)

// Lookup returns the release for the repository path, then for its directory name.
func (t Manifest) Lookup(relative, name string) (*Release, bool) {
	v, ok := t[filepath.ToSlash(relative)]
	if !ok {
		v, ok = t[name]
	}

	return v, ok
}

func LoadManifest(path string) (Manifest, error) {
	values := map[string]string{}
	if err := json.Unmarshal(defaults, &values); err != nil {
		return nil, err
	}

	if path != "" {
		overrides, err := readOverrides(path)
		if err != nil {
			return nil, err
		}

		maps.Copy(values, overrides)
	}

	result := make(Manifest, len(values))
	for k, v := range values {
		if k == "" || strings.TrimSpace(k) != k {
			return nil, fmt.Errorf("%w %q", errInvalidRepositoryKey, k)
		}

		// Empty override disables an obsolete built-in entry.
		if v == "" {
			continue
		}

		tag, commit, ok := strings.Cut(v, ":")
		if !ok || tag == "" || !commitPattern.MatchString(commit) || strings.HasPrefix(tag, "-") ||
			strings.ContainsAny(tag, "\n\r\t ") {
			return nil, fmt.Errorf("%s: %w, got %q", k, errExpectedTagCommit, v)
		}

		result[k] = &Release{Tag: tag, Commit: commit}
	}

	return result, nil
}

// readOverrides loads a JSON map of service versions from path.
func readOverrides(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = f.Close()
	}() // Read-only file; contents are checked below.

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if info.Size() > 16<<20 {
		return nil, errVersionsFileTooLarge
	}

	if strings.ToLower(filepath.Ext(path)) != ".json" {
		return nil, errVersionsFileExtension
	}

	overrides, err := decodeJSONMap(f)
	if err != nil {
		return nil, fmt.Errorf("versions file %s: %w", path, err)
	}

	if overrides == nil {
		return nil, errVersionsFileEmpty
	}

	return overrides, nil
}

// decodeJSONMap reads one JSON object and rejects any trailing value.
func decodeJSONMap(r io.Reader) (map[string]string, error) {
	dec := json.NewDecoder(r)

	var overrides map[string]string

	if err := dec.Decode(&overrides); err != nil {
		return nil, err
	}

	var extra any

	err := dec.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return overrides, nil
	}

	return nil, errUnexpectedJSON
}
