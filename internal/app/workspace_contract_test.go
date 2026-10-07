package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

var rejectedProjectPaths = []string{
	"",
	".",
	"../other",
	"a/../b",
	"/tmp/repo",
	`C:\repo`,
	"a//b",
	"a/.git",
	"a/b/",
	"a\nb",
	"a/..",
}

// TestWorkspaceDecodeRejectsAmbiguity verifies that ambiguous or duplicate workspace JSON is rejected.
func TestWorkspaceDecodeRejectsAmbiguity(t *testing.T) {
	valid := `{"schema_version":1,"default_branch":"Release-26.3.0","projects":[{"path":"search/mailbek","groups":["search"]}]}`
	w, err := DecodeWorkspace(strings.NewReader(valid))

	if err != nil || w.RevisionFor(w.Projects[0]).Branch != "Release-26.3.0" {
		t.Fatalf("decode: %+v %v", w, err)
	}
	cases := []string{
		`null`, `{}`, valid + `{}`,
		strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(valid, `"path":"search/mailbek"`, `"path":"a/b","path":"search/mailbek"`, 1),
		strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"typo":true`, 1),
		strings.Replace(valid, `"groups":["search"]`, `"groups":["search","search"]`, 1),
		`{"schema_version":1,"projects":[{"path":"a/b","revision":{"branch":"main","tag":"v1"}}]}`,
		`{"schema_version":1,"projects":[{"path":"a/b","revision":{"commit":"deadbeef"}}]}`,
		`{"schema_version":1,"projects":[null]}`,
		`{"schema_version":1,"default_branch":"main","projects":[{"path":"a/b"},{"path":"a/b"}]}`,
	}
	for _, input := range cases {
		if _, err = DecodeWorkspace(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted invalid JSON: %s", input)
		}
	}

	if _, err = DecodeWorkspace(strings.NewReader(strings.Repeat(" ", workspaceMaxBytes+1))); err == nil {
		t.Fatal("accepted oversized document")
	}
}

// TestWorkspacePathsAndSelections verifies project paths and the union of path and group filters.
func TestWorkspacePathsAndSelections(t *testing.T) {
	for _, p := range rejectedProjectPaths {
		if canonicalProjectPath(p) {
			t.Fatalf("accepted %q", p)
		}
	}
	w := &WorkspaceSpec{
		SchemaVersion: 1,
		DefaultBranch: "main",
		Projects: []*ProjectSpec{
			{
				Path:   "search/mailbek",
				Groups: []string{"search"},
			},
			{
				Path:   "search/pasifae",
				Groups: []string{"search"},
			},
			{
				Path:   "storage/dos",
				Groups: []string{"storage"},
			},
		},
	}
	selected, err := w.SelectProjects([]string{"storage/dos", "storage/dos"}, []string{"search"})

	if err != nil || len(selected) != 3 || selected[0].Path != "search/mailbek" {
		t.Fatalf("selection: %+v %v", selected, err)
	}

	if _, err = w.SelectProjects(nil, []string{"serach"}); err == nil {
		t.Fatal("accepted unknown group")
	}

	if _, err = w.SelectProjects([]string{"mailbek"}, nil); err == nil {
		t.Fatal("accepted ambiguous short name")
	}
}

// TestWorkspaceReportRefusesFalseSuccess verifies that a report cannot claim success for a blocked row.
func TestWorkspaceReportRefusesFalseSuccess(t *testing.T) {
	oid := strings.Repeat("a", 40)
	makeRow := func() *WorkspaceRow {
		return &WorkspaceRow{
			Path:    "a/b",
			Outcome: "updated",
			Expected: &ResolvedRevision{
				Kind:  "branch",
				Value: "release",
				OID:   oid,
			},
			Actual: &ObservedState{
				Head:     oid,
				Branch:   "release",
				Verified: true,
			},
		}
	}
	tests := []struct {
		name string
		edit func(*WorkspaceRow)
	}{
		{
			"dirty",
			func(r *WorkspaceRow) { r.Actual.Dirty = true },
		},
		{
			"wrong branch",
			func(r *WorkspaceRow) { r.Actual.Branch = "master" },
		},
		{
			"wrong sha",
			func(r *WorkspaceRow) { r.Actual.Head = strings.Repeat("b", 40) },
		},
		{
			"unverified",
			func(r *WorkspaceRow) { r.Actual.Verified = false },
		},
		{
			"operation",
			func(r *WorkspaceRow) { r.Actual.Operation = "MERGE_HEAD" },
		},
		{
			"failed",
			func(r *WorkspaceRow) { r.ReasonCode = "git_failed" },
		},
		{
			"stash left",
			func(r *WorkspaceRow) { r.StashOID = strings.Repeat("c", 40) },
		},
		{
			"no expected",
			func(r *WorkspaceRow) { r.Expected = nil },
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := makeRow()
			tc.edit(row)
			r := &WorkspaceReport{
				Rows: []*WorkspaceRow{row},
			}
			if err := r.Finalize([]string{"a/b"}); err != nil || r.Ready {
				t.Fatalf("false readiness: %+v %v", r, err)
			}
		})
	}
	r := &WorkspaceReport{
		Rows: []*WorkspaceRow{makeRow()},
	}
	if err := r.Finalize(
		[]string{"a/b", "a/missing"},
	); err != nil || r.Ready || len(r.Rows) != 2 ||
		r.Rows[1].ReasonCode != "not_observed" {
		t.Fatalf("missing repository disappeared: %+v %v", r, err)
	}
	r = &WorkspaceReport{
		DryRun: true,
		Rows:   []*WorkspaceRow{makeRow()},
	}
	if err := r.Finalize([]string{"a/b"}); err != nil || r.Ready {
		t.Fatal("dry-run reported ready")
	}
	r = &WorkspaceReport{
		Rows: []*WorkspaceRow{makeRow(), makeRow()},
	}
	if err := r.Finalize([]string{"a/b"}); err == nil {
		t.Fatal("duplicate report row accepted")
	}
	r = new(WorkspaceReport)
	if err := r.Finalize(nil); err != nil || r.Ready {
		t.Fatal("empty selection reported ready")
	}
}

// TestWorkspaceJSONRoundTrip verifies that a report survives a JSON round trip.
func TestWorkspaceJSONRoundTrip(t *testing.T) {
	oid := strings.Repeat("a", 40)
	r := &WorkspaceReport{
		Mode:      "status",
		Freshness: "cached",
		Rows: []*WorkspaceRow{
			{
				Path:    "a/b",
				Outcome: "observed",
				Expected: &ResolvedRevision{
					Kind:  "branch",
					Value: "release",
					OID:   oid,
				},
				Actual: &ObservedState{
					Head:     oid,
					Branch:   "release",
					Verified: true,
				},
				Message: strings.Repeat("detail\n", 1000),
			},
		},
	}

	if err := r.Finalize([]string{"a/b"}); err != nil || !r.Ready {
		t.Fatal(err)
	}

	var out bytes.Buffer

	if err := WriteWorkspaceReport(&out, r); err != nil || !json.Valid(out.Bytes()) {
		t.Fatal(err)
	}

	var decoded WorkspaceReport

	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || decoded.Rows[0].Message != r.Rows[0].Message {
		t.Fatal("JSON truncated or invalid", err)
	}
}
