package app

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadDefaultsOverrideAndLookup verifies built-in versions, file overrides, and lookup by path or name.
func TestLoadDefaultsOverrideAndLookup(t *testing.T) {
	table, e := LoadManifest("")
	if e != nil || len(table) != 43 || table["hydra"].Commit != "8284453" {
		t.Fatal(table, e)
	}

	p := filepath.Join(t.TempDir(), "versions.json")
	if writeErr := os.WriteFile(
		p,
		[]byte(`{"hydra":"","group/service":"v2:123abcd","service":"v1:abc1234"}`),
		0o600,
	); writeErr != nil {
		t.Fatal(writeErr)
	}

	table, e = LoadManifest(p)
	if e != nil {
		t.Fatal(e)
	}

	if _, ok := table["hydra"]; ok {
		t.Fatal("empty override didn't disable default")
	}

	v, _ := table.Lookup("group/service", "service")
	if v.Tag != "v2" {
		t.Fatal(v)
	}
}

// TestRejectInvalidVersions verifies that a broken versions file is rejected.
func TestRejectInvalidVersions(t *testing.T) {
	inputs := []string{
		`{"s":"v1:--help"}`,
		`{"s":"v1:abc"}`,
		`{"s":"x"}`,
		`null`,
		`{} {}`,
		`{"s":"v1:abc1234"} garbage`,
	}

	for _, input := range inputs {
		p := filepath.Join(t.TempDir(), "bad.json")
		if writeErr := os.WriteFile(p, []byte(input), 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}

		if _, e := LoadManifest(p); e == nil {
			t.Fatal("accepted", input)
		}
	}
}
