package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// ensureProjectNames fills empty names from the path and keeps names that are already set.
func ensureProjectNames(projects []*ProjectSpec) {
	used := make(map[string]struct{}, len(projects))

	for _, project := range projects {
		if project != nil && project.Name != "" {
			used[project.Name] = struct{}{}
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
func uniqueProjectName(path string, used map[string]struct{}) string {
	base := path
	if _, rest, found := strings.CutLast(path, "/"); found {
		base = rest
	}

	base = strings.NewReplacer("/", "-", "\\", "-").Replace(base)
	if base == "" || base == reservedProjectName {
		base = "project"
	}

	if _, taken := used[base]; !taken {
		used[base] = struct{}{}

		return base
	}

	sum := sha256.Sum256([]byte(path))

	prefix := base + "-" + hex.EncodeToString(sum[:4])
	name := prefix

	for suffix := 2; ; suffix++ {
		if _, taken := used[name]; !taken {
			break
		}

		name = prefix + "-" + strconv.Itoa(suffix)
	}

	used[name] = struct{}{}

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

// encodeEdited makes the project list match spec and updates the release label.
func encodeEdited(root *yaml.Node, spec *WorkspaceSpec) ([]byte, error) {
	next := copyNode(root)
	setRelease(next, spec.Release)

	if err := dropAbsentProjects(next, spec); err != nil {
		return nil, err
	}

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

	if spec.LogLevel != "" {
		block.Content = append(block.Content, strNode("log-level", false), strNode(spec.LogLevel, false))
	}

	if spec.Release != "" {
		block.Content = append(block.Content, strNode("release", false), strNode(spec.Release, false))
	}

	if spec.BaseDir != "" {
		block.Content = append(block.Content, strNode("base-dir", false), strNode(spec.BaseDir, true))
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

	if project.Archived {
		node.Content = append(node.Content, strNode(keyUserdata, false), archivedUserdata())
	}

	return node
}

// archivedUserdata is the west userdata block that records a GitLab archive mark.
func archivedUserdata() *yaml.Node {
	flag := mappingNode()
	flag.Content = append(flag.Content, strNode("archived", false), boolNode(true))
	block := mappingNode()
	block.Content = append(block.Content, strNode(keyReleaseAlign, false), flag)

	return block
}

// gitlabNode writes the discovery scope.
func gitlabNode(source *GitLabSource) *yaml.Node {
	node := mappingNode()
	node.Content = append(node.Content, strNode(keyURL, false), strNode(source.URL, false))

	if len(source.Groups) > 0 {
		node.Content = append(node.Content, strNode(keyGroups, false), stringSeq(source.Groups))
	}

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
	addTimeout(node, timeoutCatalog, timeouts.Catalog)
	addTimeout(node, timeoutCatalogBudget, timeouts.CatalogBudget)
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

// dropAbsentProjects removes sequence entries whose path is no longer in spec.
func dropAbsentProjects(root *yaml.Node, spec *WorkspaceSpec) error {
	projects, err := projectSequence(root)
	if err != nil {
		return err
	}

	wanted := make(map[string]struct{}, len(spec.Projects))

	for _, project := range spec.Projects {
		if project == nil || project.Path == "" {
			return errWorkspaceSchema
		}

		wanted[project.Path] = struct{}{}
	}

	kept := make([]*yaml.Node, 0, len(projects.Content))

	for _, item := range projects.Content {
		identity, identityErr := projectIdentity(item)
		if identityErr != nil {
			return identityErr
		}

		if identity.path == "" {
			return errWorkspaceSchema
		}

		if _, keep := wanted[identity.path]; keep {
			kept = append(kept, item)
		}
	}

	projects.Content = kept

	return nil
}

// writeArchiveMarks updates userdata.release-align.archived for projects the catalog returned.
func writeArchiveMarks(root *yaml.Node, byPath map[string]bool) (bool, error) {
	sequence, err := projectSequence(root)
	if err != nil {
		return false, err
	}

	changed := false

	for _, item := range sequence.Content {
		identity, identityErr := projectIdentity(item)
		if identityErr != nil {
			return false, identityErr
		}

		archived, found := byPath[identity.path]
		if !found {
			continue
		}

		updated, flagErr := setArchivedFlag(item, archived)
		if flagErr != nil {
			return false, flagErr
		}

		changed = changed || updated
	}

	return changed, nil
}

// setArchivedFlag writes or clears one archive mark without replacing other userdata.
func setArchivedFlag(node *yaml.Node, archived bool) (bool, error) {
	user, ok := mappingValue(node, keyUserdata)
	if !ok {
		if !archived {
			return false, nil
		}

		node.Content = append(node.Content, strNode(keyUserdata, false), archivedUserdata())

		return true, nil
	}

	if user.Kind != yaml.MappingNode {
		return false, fmt.Errorf("%w: userdata", errWorkspaceField)
	}

	block, ok := mappingValue(user, keyReleaseAlign)
	if !ok {
		if !archived {
			return false, nil
		}

		flag := mappingNode()
		flag.Content = append(flag.Content, strNode("archived", false), boolNode(true))
		user.Content = append(user.Content, strNode(keyReleaseAlign, false), flag)

		return true, nil
	}

	if block.Kind != yaml.MappingNode {
		return false, fmt.Errorf("%w: userdata.release-align", errWorkspaceField)
	}

	flag, ok := mappingValue(block, "archived")
	if !ok {
		if !archived {
			return false, nil
		}

		block.Content = append(block.Content, strNode("archived", false), boolNode(true))

		return true, nil
	}

	if flag.Kind != yaml.ScalarNode || flag.ShortTag() != yamlTagBool {
		return false, fmt.Errorf("%w: userdata.release-align.archived", errWorkspaceField)
	}

	want := yamlBoolFalse
	if archived {
		want = yamlBoolTrue
	}

	if flag.Value == want {
		return false, nil
	}

	flag.Tag = yamlTagBool
	flag.Value = want

	return true, nil
}

// projectSequence returns the manifest project list.
func projectSequence(root *yaml.Node) (*yaml.Node, error) {
	manifest, ok := mappingValue(root, keyManifest)
	if !ok {
		return nil, errWorkspaceSchema
	}

	projects, ok := mappingValue(manifest, keyProjects)
	if !ok || projects.Kind != yaml.SequenceNode {
		return nil, errWorkspaceSchema
	}

	return projects, nil
}

// appendMissingProjects adds inventory rows that are not already in the document.
func appendMissingProjects(root *yaml.Node, spec *WorkspaceSpec) error {
	projects, err := projectSequence(root)
	if err != nil {
		return err
	}

	existing := make(map[string]struct{}, len(projects.Content))

	for _, item := range projects.Content {
		identity, identityErr := projectIdentity(item)
		if identityErr != nil {
			return identityErr
		}

		existing[identity.path] = struct{}{}
	}

	for _, project := range spec.Projects {
		if project == nil {
			continue
		}

		if _, present := existing[project.Path]; present {
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

// boolNode is a boolean scalar.
func boolNode(value bool) *yaml.Node {
	text := yamlBoolFalse
	if value {
		text = yamlBoolTrue
	}

	node := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   yamlTagBool,
		Value: text,
	}

	return node
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
