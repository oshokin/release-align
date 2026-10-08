package app

import (
	"strings"
)

// plan resolves every selected revision before the first git archive.
func (j *archiveJob) plan() ([]*plannedRepo, error) {
	selected, err := j.spec.SelectProjects(j.opts.Repositories, j.opts.Groups)
	if err != nil {
		return nil, err
	}

	planned := make([]*plannedRepo, 0, len(selected))

	for _, project := range selected {
		repo, planErr := j.planOne(project)
		if planErr != nil {
			return nil, planErr
		}

		planned = append(planned, repo)
	}

	return planned, nil
}

// planOne resolves one project path to a full commit and lists its gitlinks.
func (j *archiveJob) planOne(project *ProjectSpec) (*plannedRepo, error) {
	if err := j.ctx.Err(); err != nil {
		return nil, err
	}

	dir, err := ResolveProjectDirectory(j.base, project.Path)
	if err != nil {
		return nil, archivePhase(project.Path, "resolve", err)
	}

	if err = sameWorktreeRoot(j.ctx, j.git, dir); err != nil {
		return nil, archivePhase(project.Path, "resolve", err)
	}

	revision := j.spec.RevisionFor(project)

	resolved, err := ResolveRevision(j.ctx, j.git, dir, revision)
	if err != nil {
		return nil, archivePhase(project.Path, "resolve", err)
	}

	links, err := j.gitlinks(dir, project.Path, resolved.OID)
	if err != nil {
		return nil, err
	}

	return &plannedRepo{
		Path:      project.Path,
		Requested: j.requestedRevision(revision),
		Commit:    resolved.OID,
		Dir:       dir,
		Gitlinks:  links,
	}, nil
}

// gitlinks reads mode 160000 entries from the resolved tree.
func (j *archiveJob) gitlinks(dir, project, oid string) ([]*archiveGitlink, error) {
	out, err := j.git.Local(j.ctx, dir, "ls-tree", "-r", "-z", oid)
	if err != nil {
		return nil, archivePhase(project, "ls-tree", err)
	}

	return j.parseGitlinks(out), nil
}

// requestedRevision is the branch, tag, or commit stored in the workspace.
func (j *archiveJob) requestedRevision(spec *RevisionSpec) string {
	switch {
	case spec == nil:
		return ""
	case spec.Commit != "":
		return spec.Commit
	case spec.Tag != "":
		return spec.Tag
	default:
		return spec.Branch
	}
}

// parseGitlinks reads NUL-separated ls-tree records.
func (j *archiveJob) parseGitlinks(out string) []*archiveGitlink {
	var links []*archiveGitlink

	for part := range strings.SplitSeq(out, "\x00") {
		link := j.parseGitlink(part)
		if link == nil {
			continue
		}

		links = append(links, link)
	}

	return links
}

// parseGitlink returns a gitlink record, or nil for every other ls-tree line.
func (j *archiveJob) parseGitlink(part string) *archiveGitlink {
	if part == "" {
		return nil
	}

	meta, linkPath, ok := strings.Cut(part, "\t")
	if !ok {
		return nil
	}

	fields := strings.Fields(meta)
	if len(fields) < 3 || fields[0] != "160000" {
		return nil
	}

	return &archiveGitlink{
		Path:   linkPath,
		Commit: fields[2],
	}
}
