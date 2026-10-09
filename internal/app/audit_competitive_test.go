package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/oshokin/release-align/internal/gitter"
)

type auditOriginFailureGit struct{ *gitter.Client }

var errAuditOriginIO = errors.New("injected operational I/O failure")

func BenchmarkAuditArchiveNames(b *testing.B) {
	for _, size := range []int{1000, 5000, 10000} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			paths := make([]string, size)
			for i := range paths {
				paths[i] = "mailion/service/file-" + strconv.Itoa(i)
			}

			b.ResetTimer()

			for range b.N {
				names := newArchiveNames()
				for _, path := range paths {
					if _, err := names.add(path, 0o644); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

func TestAuditOversizedDocumentRejected(t *testing.T) {
	body := "manifest:\n  projects:\n    - name: svc\n"
	body += strings.Repeat("\n", workspaceMaxBytes) + "# tail beyond read limit\n"
	file := filepath.Join(t.TempDir(), "workspace.yml")
	write(t, file, body)

	if _, err := LoadWorkspace(file); !errors.Is(err, errWorkspaceSize) {
		t.Fatalf("baseline bounded reader: %v", err)
	}

	if _, err := readWorkspaceDocument(file); !errors.Is(err, errWorkspaceSize) {
		t.Fatalf("writer's reader accepted truncated input: %v", err)
	}
}

func TestAuditRefreshMustNotDiscardOversizedTail(t *testing.T) {
	f := setup(t)
	body := "manifest:\n  projects:\n    - name: old\n      path: group/old\n"
	body += strings.Repeat("\n", workspaceMaxBytes) + "# KEEP THIS TAIL\n"
	file := filepath.Join(t.TempDir(), "workspace.yml")
	write(t, file, body)
	opts := &WorkspaceRefreshOptions{
		BaseDir: f.base,
		Sync:    true,
	}
	report, err := RefreshWorkspace(t.Context(), offlineClient(f), file, opts)

	after, readErr := os.ReadFile(file)
	if readErr != nil {
		t.Fatal(readErr)
	}

	if err == nil || string(after) != body {
		t.Fatalf(
			"refresh accepted oversized YAML and/or rewrote it: err=%v written=%v before=%d after=%d",
			err,
			report != nil && report.Written,
			len(body),
			len(after),
		)
	}
}

func TestAuditNumericWestVersion(t *testing.T) {
	for _, version := range []string{"1.0", "1.2", "0.13", "0.10", `"1.0"`} {
		t.Run(version, func(t *testing.T) {
			body := "manifest:\n  version: " + version + "\n  projects:\n    - name: svc\n      url: https://example.invalid/svc\n"
			if _, err := DecodeWorkspace(strings.NewReader(body)); err != nil {
				t.Fatal(err)
			}
		})
	}

	for _, version := range []string{"true", "null", "9.9", "[1]"} {
		t.Run("reject-"+version, func(t *testing.T) {
			body := "manifest:\n  version: " + version + "\n  projects:\n    - name: svc\n"
			if _, err := DecodeWorkspace(strings.NewReader(body)); err == nil {
				t.Fatal("accepted unsupported version")
			}
		})
	}
}

func (g *auditOriginFailureGit) Local(ctx context.Context, dir string, args ...string) (string, error) {
	if strings.Join(args, " ") == "remote get-url origin" {
		return "", errAuditOriginIO
	}

	return g.Client.Local(ctx, dir, args...)
}

func TestAuditOriginOperationalErrorIsNotMissingOrigin(t *testing.T) {
	f := setup(t)
	spec := oneProject(t, "group/repo with spaces", nil)
	items := newWorkspaceItems(spec, spec.Projects)
	g := &auditOriginFailureGit{Client: offlineClient(f)}
	r := &runner{cfg: f.cfg, git: g}

	if err := r.bindWorkspaceDirs(t.Context(), items); err != nil {
		t.Fatal(err)
	}

	if items[0].row.ReasonCode != reasonGitFailed {
		t.Fatalf("operational failure misclassified: %+v", items[0].row)
	}
}

func TestAuditMissingShortRevisionKeepsCoverage(t *testing.T) {
	f := setup(t)
	body := "manifest:\n  defaults:\n    revision: master\n  projects:\n    - name: existing\n      path: group/repo with spaces\n    - name: missing\n      path: group/missing\n"

	spec, err := DecodeWorkspace(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	report, err := RunWorkspace(t.Context(), f.cfg, spec, ModeStatus)
	if err == nil {
		t.Fatal("missing checkout must not be ready")
	}

	if report.ExpectedCount != 2 || len(report.Rows) != 2 || !report.Coverage {
		t.Fatalf("all selected rows lost on missing short ref: %+v; %v", report, err)
	}
}

func TestAuditContractRejectWrongRepository(t *testing.T) {
	for _, mode := range []string{ModeStatus, ModeSync, "archive"} {
		t.Run(mode, func(t *testing.T) {
			f := setup(t)
			spec := oneProject(t, "group/repo with spaces", nil)
			spec.Projects[0].URL = "https://expected.example.invalid/mailion/correct-service.git"

			if mode == "archive" {
				file := saveWorkspace(t, spec)
				dest := filepath.Join(t.TempDir(), "wrong.zip")
				input := &archiveOptInput{
					fixture:   f,
					workspace: file,
					dest:      dest,
				}

				report, err := ArchiveWorkspace(t.Context(), archiveOpts(input))
				if err == nil {
					t.Fatalf("archived wrong repository: %+v", report)
				}

				return
			}

			report, err := RunWorkspace(t.Context(), f.cfg, spec, mode)
			if err == nil || report.Ready {
				t.Fatalf("wrong repository reported ready: %+v, %v", report, err)
			}
		})
	}
}

func TestAuditCloneAlwaysCreatesOrigin(t *testing.T) {
	f := setup(t)
	git(t, f.seed, "config", "--global", "clone.defaultRemoteName", "upstream")
	dir := filepath.Join(t.TempDir(), "new-clone")
	client := offlineClient(f)

	if err := client.Clone(t.Context(), f.remote, dir, f.cfg.CloneTimeout); err != nil {
		t.Fatal(err)
	}

	if _, err := client.Local(t.Context(), dir, "remote", "get-url", "origin"); err != nil {
		t.Fatalf("clone command made a checkout that release-align cannot sync: %v", err)
	}
}
