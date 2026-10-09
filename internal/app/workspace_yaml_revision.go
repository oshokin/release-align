package app

import (
	"context"
	"fmt"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/oshokin/release-align/internal/gitter"
)

// parsedRevision is a typed pin or a short name still to be classified.
type parsedRevision struct {
	// revision is a branch, tag, or full commit. Nil when short is set.
	revision *RevisionSpec
	// short is an unclassified name. Empty when revision is set.
	short string
}

// revisionLookup is the local Git data used to classify short west names.
type revisionLookup struct {
	// git reads cached refs and does not fetch.
	git LocalGit
	// base is the workspace root.
	base string
	// spec supplies the inherited short default.
	spec *WorkspaceSpec
	// projects are the selected repositories.
	projects []*ProjectSpec
}

// ResolveWorkspaceRevisions classifies short west names from local branch and tag refs.
// It does not fetch. Full refs and commits are left unchanged.
func ResolveWorkspaceRevisions(ctx context.Context, lookup *revisionLookup) error {
	for _, project := range lookup.projects {
		if err := resolveProjectRevision(ctx, lookup, project); err != nil {
			return err
		}
	}

	return nil
}

// resolveProjectRevision looks up one project's short revision in that repository.
func resolveProjectRevision(ctx context.Context, lookup *revisionLookup, project *ProjectSpec) error {
	if project == nil || project.Revision != nil {
		return nil
	}

	short := project.shortRevision
	if short == "" {
		short = lookup.spec.shortDefault
	}

	if short == "" {
		return nil
	}

	dir, err := ResolveProjectDirectory(lookup.base, project.Path)
	if err != nil {
		return fmt.Errorf("%s: %w", project.Path, errWorkspaceRevisionShort)
	}

	revision, err := classifyShortRevision(ctx, lookup.git, dir, short)
	if err != nil {
		return fmt.Errorf("%s: %w", project.Path, err)
	}

	project.resolvedRevision = revision

	return nil
}

// classifyShortRevision accepts exactly one of refs/heads or refs/tags.
func classifyShortRevision(ctx context.Context, g LocalGit, dir, name string) (*RevisionSpec, error) {
	branch, err := refExists(ctx, g, dir, refHeads+name)
	if err != nil {
		return nil, err
	}

	if !branch {
		branch, err = refExists(ctx, g, dir, "refs/remotes/origin/"+name)
		if err != nil {
			return nil, err
		}
	}

	tag, err := refExists(ctx, g, dir, refTags+name)
	if err != nil {
		return nil, err
	}

	switch {
	case branch && !tag:
		revision := &RevisionSpec{
			Branch: name,
		}

		return revision, nil
	case tag && !branch:
		revision := &RevisionSpec{
			Tag: name,
		}

		return revision, nil
	default:
		return nil, fmt.Errorf("%w: %s", errWorkspaceRevisionShort, name)
	}
}

// refExists reports whether one exact ref is already local.
func refExists(ctx context.Context, g LocalGit, dir, ref string) (bool, error) {
	_, err := g.Local(ctx, dir, "show-ref", "--verify", "--quiet", ref)
	if err == nil {
		return true, nil
	}

	if gitter.ExitCode(err) == 1 {
		return false, nil
	}

	return false, err
}

// optionalRevision reads a project revision. Absence inherits the default.
func optionalRevision(node *yaml.Node) (*parsedRevision, error) {
	value, ok := mappingValue(node, keyRevision)
	if !ok {
		empty := new(parsedRevision)

		return empty, nil
	}

	return parseRevision(value)
}

// parseRevision classifies refs/heads, refs/tags, a full id, or a short name.
func parseRevision(node *yaml.Node) (*parsedRevision, error) {
	if node == nil || node.Kind != yaml.ScalarNode ||
		(node.ShortTag() != yamlTagString && !numericCommitNode(node)) {
		line := 0
		if node != nil {
			line = node.Line
		}

		return nil, fmt.Errorf("%w: line %d", errWorkspaceRevisionType, line)
	}

	value := node.Value
	switch {
	case strings.HasPrefix(value, refHeads):
		revision := &RevisionSpec{
			Branch: strings.TrimPrefix(value, refHeads),
		}
		if err := revision.Validate(); err != nil {
			return nil, err
		}

		parsed := &parsedRevision{revision: revision}

		return parsed, nil
	case strings.HasPrefix(value, refTags):
		revision := &RevisionSpec{
			Tag: strings.TrimPrefix(value, refTags),
		}
		if err := revision.Validate(); err != nil {
			return nil, err
		}

		parsed := &parsedRevision{revision: revision}

		return parsed, nil
	case workspaceOID.MatchString(value):
		revision := &RevisionSpec{
			Commit: value,
		}
		parsed := &parsedRevision{revision: revision}

		return parsed, nil
	default:
		if revisionExpression(value) {
			return nil, fmt.Errorf("%w: line %d", errWorkspaceRevisionShort, node.Line)
		}

		parsed := &parsedRevision{short: value}

		return parsed, nil
	}
}

// numericCommitNode permits full decimal-looking object ids without numeric conversion.
func numericCommitNode(node *yaml.Node) bool {
	if node == nil || !workspaceOID.MatchString(node.Value) {
		return false
	}

	return node.ShortTag() == yamlTagInt || node.ShortTag() == yamlTagFloat
}

// revisionExpression reports a west expression this reader does not classify locally.
func revisionExpression(value string) bool {
	return value == "" || strings.HasPrefix(value, "-") || strings.Contains(value, "..") ||
		strings.Contains(value, "@{") || strings.ContainsAny(value, " \t~^:")
}
