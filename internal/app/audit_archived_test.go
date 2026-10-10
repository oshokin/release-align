package app

import (
	"strings"
	"testing"

	"github.com/oshokin/release-align/internal/gitlab"
)

func TestAuditArchivedMarkRoundTrip(t *testing.T) {
	body := "manifest:\n  version: \"1.0\"\n  projects:\n    - name: legacy\n      path: mailion/search/legacy\n" +
		"      userdata:\n        release-align:\n          archived: true\n"

	spec, err := DecodeWorkspace(strings.NewReader(body))
	if err != nil || !spec.Projects[0].Archived {
		t.Fatalf("archived mark: %v %+v", err, spec)
	}
}

func TestAuditArchivedSelectionDoesNotBlock(t *testing.T) {
	f := setup(t)
	body := "manifest:\n  defaults:\n    revision: refs/heads/master\n  projects:\n" +
		"    - name: live\n      path: group/repo with spaces\n" +
		"    - name: old\n      path: group/missing-archived\n      userdata:\n        release-align:\n          archived: true\n"

	spec, err := DecodeWorkspace(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	report, err := RunWorkspace(t.Context(), f.cfg, spec, ModeStatus)
	if err != nil || !report.Ready || report.SkippedArchivedCount != 1 || report.ActionableCount != 1 {
		t.Fatalf("archived row blocked the active project: %+v %v", report, err)
	}
}

func TestAuditAllArchivedIsANoOp(t *testing.T) {
	f := setup(t)
	body := "manifest:\n  defaults:\n    revision: refs/heads/master\n  projects:\n" +
		"    - name: old\n      path: group/missing-archived\n      userdata:\n        release-align:\n          archived: true\n"

	spec, err := DecodeWorkspace(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	report, err := RunWorkspace(t.Context(), f.cfg, spec, ModeSync)
	if err != nil || report.Ready || report.ActionableCount != 0 || report.SkippedArchivedCount != 1 {
		t.Fatalf("all-archived run: %+v %v", report, err)
	}
}

func TestAuditExplicitArchivedCloneRequiresOptIn(t *testing.T) {
	project := &gitlab.Project{
		PathWithNamespace: "mailion/search/legacy",
		Archived:          true,
	}

	job := new(cloneJob)
	accepted, err := job.acceptArchivedClone(project, false, true)

	if err == nil || accepted {
		t.Fatal(accepted, err)
	}

	accepted, err = job.acceptArchivedClone(project, true, true)
	if err != nil || !accepted {
		t.Fatal(accepted, err)
	}

	accepted, err = job.acceptArchivedClone(project, false, false)
	if err != nil || accepted {
		t.Fatal(accepted, err)
	}
}
