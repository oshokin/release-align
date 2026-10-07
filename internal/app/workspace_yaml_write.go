package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ensureProjectNames fills empty names from the path and keeps names that are already set.
func ensureProjectNames(projects []*ProjectSpec) {
	used := make(map[string]bool, len(projects))

	for _, project := range projects {
		if project != nil && project.Name != "" {
			used[project.Name] = true
		}
	}

	for _, project := range projects {
		if project == nil || project.Name != "" {
			continue
		}

		project.Name = uniqueProjectName(project.Path, used)
	}
}

// uniqueProjectName uses the path basename, then a stable suffix when that name is taken.
func uniqueProjectName(path string, used map[string]bool) string {
	base := path
	if _, rest, found := strings.CutLast(path, "/"); found {
		base = rest
	}

	base = strings.NewReplacer("/", "-", "\\", "-").Replace(base)
	if base == "" || base == reservedProjectName {
		base = "project"
	}

	if !used[base] {
		used[base] = true

		return base
	}

	sum := sha256.Sum256([]byte(path))

	prefix := base + "-" + hex.EncodeToString(sum[:4])
	name := prefix

	for suffix := 2; used[name]; suffix++ {
		name = prefix + "-" + strconv.Itoa(suffix)
	}

	used[name] = true

	return name
}

// encodeFresh builds a new release-align YAML document from a validated inventory.
func encodeFresh(spec *WorkspaceSpec) ([]byte, error) {
	ensureProjectNames(spec.Projects)

	root := &yaml.Node{
		Kind: yaml.MappingNode,
	}
	root.Content = append(
		root.Content,
		strNode(keyManifest, false),
		manifestNode(spec),
		strNode(keyReleaseAlign, false),
		releaseNode(spec),
	)

	return marshalWorkspaceYAML(root)
}

// encodeEdited appends projects that are not in the node and updates the release label.
func encodeEdited(root *yaml.Node, spec *WorkspaceSpec) ([]byte, error) {
	next := copyNode(root)
	setRelease(next, spec.Release)

	if err := appendMissingProjects(next, spec); err != nil {
		return nil, err
	}

	return marshalWorkspaceYAML(next)
}

// marshalWorkspaceYAML uses the same two-space indentation as the documented examples.
func marshalWorkspaceYAML(root *yaml.Node) ([]byte, error) {
	out := &bytes.Buffer{}
	encoder := yaml.NewEncoder(out)
	encoder.SetIndent(2)

	if err := encoder.Encode(root); err != nil {
		return nil, err
	}

	if err := encoder.Close(); err != nil {
		return nil, err
	}

	return out.Bytes(), nil
}

// manifestNode is the west manifest written by init.
func manifestNode(spec *WorkspaceSpec) *yaml.Node {
	manifest := mappingNode()
	manifest.Content = append(manifest.Content, strNode("version", false), strNode("1.0", true))

	if spec.DefaultRevision != nil || spec.shortDefault != "" {
		defaults := mappingNode()
		text, quote := spec.shortDefault, false

		if spec.DefaultRevision != nil {
			text, quote = revisionText(spec.DefaultRevision)
		}

		defaults.Content = append(defaults.Content, strNode(keyRevision, false), strNode(text, quote))
		manifest.Content = append(manifest.Content, strNode("defaults", false), defaults)
	}

	if len(spec.GroupFilter) > 0 {
		filters := make([]string, 0, len(spec.GroupFilter))
		for _, filter := range spec.GroupFilter {
			sign := "+"
			if filter.Disable {
				sign = "-"
			}

			filters = append(filters, sign+filter.Name)
		}

		manifest.Content = append(manifest.Content, strNode("group-filter", false), stringSeq(filters))
	}

	projects := &yaml.Node{
		Kind: yaml.SequenceNode,
	}

	for _, project := range spec.Projects {
		projects.Content = append(projects.Content, projectNode(project))
	}

	manifest.Content = append(manifest.Content, strNode(keyProjects, false), projects)

	return manifest
}

// releaseNode is the release-align block written by init.
func releaseNode(spec *WorkspaceSpec) *yaml.Node {
	block := mappingNode()
	block.Content = append(block.Content, strNode("schema-version", false), intNode(1))

	if spec.Release != "" {
		block.Content = append(block.Content, strNode("release", false), strNode(spec.Release, false))
	}

	if spec.GitLab != nil {
		block.Content = append(block.Content, strNode("gitlab", false), gitlabNode(spec.GitLab))
	}

	if spec.Timeouts != nil {
		block.Content = append(block.Content, strNode("timeouts", false), timeoutsNode(spec.Timeouts))
	}

	return block
}

// projectNode is one project in the canonical field order.
func projectNode(project *ProjectSpec) *yaml.Node {
	node := mappingNode()
	node.Content = append(
		node.Content,
		strNode(keyName, false),
		strNode(project.Name, false),
		strNode(keyPath, false),
		strNode(project.Path, false),
	)

	if project.URL != "" {
		node.Content = append(node.Content, strNode(keyURL, false), strNode(project.URL, false))
	}

	if len(project.Groups) > 0 {
		node.Content = append(node.Content, strNode(keyGroups, false), stringSeq(project.Groups))
	}

	if project.Revision != nil {
		text, quote := revisionText(project.Revision)
		node.Content = append(node.Content, strNode(keyRevision, false), strNode(text, quote))
	} else if project.shortRevision != "" {
		node.Content = append(node.Content, strNode(keyRevision, false), strNode(project.shortRevision, false))
	}

	if project.CloneDepth != nil {
		node.Content = append(node.Content, strNode("clone-depth", false), intNode(*project.CloneDepth))
	}

	return node
}

// gitlabNode writes the discovery scope.
func gitlabNode(source *GitLabSource) *yaml.Node {
	node := mappingNode()
	node.Content = append(
		node.Content,
		strNode(keyURL, false),
		strNode(source.URL, false),
		strNode(keyGroups, false),
		stringSeq(source.Groups),
	)

	protocol := source.CloneProtocol
	if protocol == "" {
		protocol = cloneProtocolSSH
	}

	node.Content = append(node.Content, strNode("clone-protocol", false), strNode(protocol, false))

	return node
}

// timeoutsNode writes only the overrides that are set.
func timeoutsNode(timeouts *WorkspaceTimeouts) *yaml.Node {
	node := mappingNode()
	addTimeout(node, "probe", timeouts.Probe)
	addTimeout(node, "fetch", timeouts.Fetch)
	addTimeout(node, "local", timeouts.Local)
	addTimeout(node, "clone", timeouts.Clone)
	addTimeout(node, "archive", timeouts.Archive)

	return node
}

// addTimeout appends one duration when it was set.
func addTimeout(node *yaml.Node, key string, value *string) {
	if value == nil {
		return
	}

	node.Content = append(node.Content, strNode(key, false), strNode(*value, false))
}

// revisionText returns the west spelling and whether a commit must be quoted.
func revisionText(revision *RevisionSpec) (string, bool) {
	switch {
	case revision.Branch != "":
		return refHeads + revision.Branch, false
	case revision.Tag != "":
		return refTags + revision.Tag, false
	default:
		return revision.Commit, true
	}
}

// appendMissingProjects adds inventory rows that are not already in the document.
func appendMissingProjects(root *yaml.Node, spec *WorkspaceSpec) error {
	manifest, ok := mappingValue(root, keyManifest)
	if !ok {
		return errWorkspaceSchema
	}

	projects, ok := mappingValue(manifest, keyProjects)
	if !ok || projects.Kind != yaml.SequenceNode {
		return errWorkspaceSchema
	}

	existing := make(map[string]bool, len(projects.Content))

	for _, item := range projects.Content {
		path, err := optionalString(item, keyPath)
		if err != nil {
			return err
		}

		if path == "" {
			path, err = optionalString(item, keyName)
			if err != nil {
				return err
			}
		}

		existing[path] = true
	}

	for _, project := range spec.Projects {
		if project == nil || existing[project.Path] {
			continue
		}

		projects.Content = append(projects.Content, projectNode(project))
	}

	return nil
}

// setRelease replaces the release label without dropping the surrounding node.
func setRelease(root *yaml.Node, release string) {
	block, ok := mappingValue(root, keyReleaseAlign)
	if !ok || block.Kind != yaml.MappingNode {
		return
	}

	value, ok := mappingValue(block, "release")
	if !ok {
		if release == "" {
			return
		}

		block.Content = append(block.Content, strNode("release", false), strNode(release, false))

		return
	}

	value.SetString(release)
}

// copyNode duplicates a YAML tree so a failed publication does not edit the original.
func copyNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}

	next := &yaml.Node{
		Kind:        node.Kind,
		Style:       node.Style,
		Tag:         node.Tag,
		Value:       node.Value,
		Anchor:      node.Anchor,
		Alias:       node.Alias,
		HeadComment: node.HeadComment,
		LineComment: node.LineComment,
		FootComment: node.FootComment,
		Line:        node.Line,
		Column:      node.Column,
	}

	if len(node.Content) == 0 {
		return next
	}

	next.Content = make([]*yaml.Node, len(node.Content))

	for i, child := range node.Content {
		next.Content[i] = copyNode(child)
	}

	return next
}

// mappingNode is an empty YAML mapping.
func mappingNode() *yaml.Node {
	node := &yaml.Node{
		Kind: yaml.MappingNode,
	}

	return node
}

// strNode is a string scalar. quote forces double quotes for versions and object ids.
func strNode(value string, quote bool) *yaml.Node {
	node := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   yamlTagString,
		Value: value,
	}
	if quote || legacyYAMLScalar(value) {
		node.Style = yaml.DoubleQuotedStyle
		node.Tag = yamlTagString
	}

	return node
}

// legacyYAMLScalar quotes values interpreted differently by west's YAML 1.1 reader.
func legacyYAMLScalar(value string) bool {
	switch strings.ToLower(value) {
	case "y", "n", "yes", "no", "on", "off":
		return true
	default:
		return strings.Contains(value, ":") && value != "" && value[0] >= '0' && value[0] <= '9'
	}
}

// intNode is an integer scalar.
func intNode(value int) *yaml.Node {
	node := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   yamlTagInt,
		Value: strconv.Itoa(value),
	}

	return node
}

// stringSeq is a flow-unspecified sequence of strings.
func stringSeq(values []string) *yaml.Node {
	node := &yaml.Node{
		Kind:    yaml.SequenceNode,
		Style:   yaml.FlowStyle,
		Content: make([]*yaml.Node, len(values)),
	}

	for i, value := range values {
		node.Content[i] = strNode(value, false)
	}

	return node
}
