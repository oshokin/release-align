package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/oshokin/release-align/internal/gitter"
)

// decodedWorkspace is a validated inventory and the YAML node that produced it.
type decodedWorkspace struct {
	// spec is the typed inventory.
	spec *WorkspaceSpec
	// root is the document mapping, kept so a later edit can preserve other nodes.
	root *yaml.Node
}

const (
	// keyManifest is the west manifest block.
	keyManifest = "manifest"
	// keyProjects is the project list inside the manifest.
	keyProjects = "projects"
	// keyRevision is a west revision scalar.
	keyRevision = "revision"
	// keyReleaseAlign is the release-align block beside the manifest.
	keyReleaseAlign = "release-align"
	// refHeads is the prefix that records a branch.
	refHeads = "refs/heads/"
	// refTags is the prefix that records a tag.
	refTags = "refs/tags/"
	// yamlTagString is a plain YAML string.
	yamlTagString = "!!str"
	// yamlTagInt is a plain YAML integer.
	yamlTagInt = "!!int"
	// keyName is a west project name.
	keyName = "name"
	// keyRemote is a west remote name.
	keyRemote = "remote"
	// keyGroups is a project group list.
	keyGroups = "groups"
	// keyDescription is optional west text kept in the node.
	keyDescription = "description"
	// keyPath is the workspace-relative project path.
	keyPath = "path"
	// keyUserdata is opaque west metadata kept in the node.
	keyUserdata = "userdata"
	// keyURL is an explicit project or GitLab URL.
	keyURL = "url"
	// keyWestCommands is a west extension path that this program does not run.
	keyWestCommands = "west-commands"
)

// DecodeWorkspace parses one YAML workspace and rejects a second document.
func DecodeWorkspace(src io.Reader) (*WorkspaceSpec, error) {
	decoded, err := decodeWorkspace(src)
	if err != nil {
		return nil, err
	}

	return decoded.spec, nil
}

// LoadWorkspace reads one bounded YAML workspace from filename.
func LoadWorkspace(filename string) (*WorkspaceSpec, error) {
	decoded, err := readWorkspace(filename)
	if err != nil {
		return nil, err
	}

	return decoded.spec, nil
}

// ResolveWorkspaceRevisions classifies short west names from local branch and tag refs.
// It does not fetch. Full refs and commits are left unchanged.
func ResolveWorkspaceRevisions(ctx context.Context, g LocalGit, base string, spec *WorkspaceSpec) error {
	if spec == nil || !spec.needsRevisionLookup() {
		return nil
	}

	for _, project := range spec.Projects {
		if err := resolveProjectRevision(ctx, g, base, spec, project); err != nil {
			return err
		}
	}

	return nil
}

// needsRevisionLookup reports a short name that is not refs/heads, refs/tags, or a full id.
func (w *WorkspaceSpec) needsRevisionLookup() bool {
	if w.shortDefault != "" {
		return true
	}

	for _, project := range w.Projects {
		if project != nil && project.shortRevision != "" {
			return true
		}
	}

	return false
}

// resolveProjectRevision looks up one project's short revision in that repository.
func resolveProjectRevision(
	ctx context.Context,
	g LocalGit,
	base string,
	spec *WorkspaceSpec,
	project *ProjectSpec,
) error {
	if project == nil || project.Revision != nil {
		return nil
	}

	short := project.shortRevision
	if short == "" {
		short = spec.shortDefault
	}

	if short == "" {
		return nil
	}

	dir, err := ResolveProjectDirectory(base, project.Path)
	if err != nil {
		return fmt.Errorf("%s: %w", project.Path, errWorkspaceRevisionShort)
	}

	revision, err := classifyShortRevision(ctx, g, dir, short)
	if err != nil {
		return fmt.Errorf("%s: %w", project.Path, err)
	}

	project.Revision = revision
	project.shortRevision = ""

	return nil
}

// classifyShortRevision accepts exactly one of refs/heads or refs/tags.
func classifyShortRevision(ctx context.Context, g LocalGit, dir, name string) (*RevisionSpec, error) {
	branch, err := refExists(ctx, g, dir, refHeads+name)
	if err != nil {
		return nil, err
	}

	tag, err := refExists(ctx, g, dir, refTags+name)
	if err != nil {
		return nil, err
	}

	switch {
	case branch && !tag:
		return &RevisionSpec{
			Branch: name,
		}, nil
	case tag && !branch:
		return &RevisionSpec{
			Tag: name,
		}, nil
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

// readWorkspace opens one regular file and decodes it.
func readWorkspace(filename string) (*decodedWorkspace, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	defer file.Close()

	return decodeWorkspace(file)
}

// decodeWorkspace reads at most 1 MiB and decodes a single YAML document.
func decodeWorkspace(src io.Reader) (*decodedWorkspace, error) {
	data, err := io.ReadAll(io.LimitReader(src, workspaceMaxBytes+1))
	if err != nil {
		return nil, err
	}

	if len(data) > workspaceMaxBytes {
		return nil, errWorkspaceSize
	}

	return decodeWorkspaceBytes(data)
}

// decodeWorkspaceBytes rejects JSON, extra documents, and unsupported YAML.
func decodeWorkspaceBytes(data []byte) (*decodedWorkspace, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] == '{' || trimmed[0] == '[' {
		return nil, errWorkspaceYAML
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))

	var document yaml.Node

	if err := dec.Decode(&document); err != nil {
		return nil, errWorkspaceYAML
	}

	var extra yaml.Node

	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, errWorkspaceYAML
	}

	if err := walkYAML(&document, 0); err != nil {
		return nil, err
	}

	root := documentContent(&document)

	spec, err := decodeRoot(root)
	if err != nil {
		return nil, err
	}

	if err = spec.Validate(); err != nil {
		return nil, err
	}

	decoded := &decodedWorkspace{
		spec: spec,
		root: root,
	}

	return decoded, nil
}

// documentContent returns the single value inside a YAML document node.
func documentContent(document *yaml.Node) *yaml.Node {
	if document != nil && document.Kind == yaml.DocumentNode && len(document.Content) == 1 {
		return document.Content[0]
	}

	return document
}

// walkYAML rejects aliases, anchors, merge keys, custom tags, and deep trees.
func walkYAML(node *yaml.Node, depth int) error {
	if node == nil {
		return nil
	}

	if depth > yamlNestLimit {
		return errWorkspaceNesting
	}

	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return errWorkspaceAnchor
	}

	if node.Kind == yaml.MappingNode {
		return walkMapping(node, depth)
	}

	if !standardTag(node) {
		return errWorkspaceAnchor
	}

	for _, child := range node.Content {
		if err := walkYAML(child, depth+1); err != nil {
			return err
		}
	}

	return nil
}

// walkMapping checks one mapping, including duplicate keys and merge keys.
func walkMapping(node *yaml.Node, depth int) error {
	if !standardTag(node) || len(node.Content)%2 != 0 {
		return errWorkspaceField
	}

	seen := make(map[string]bool, len(node.Content)/2)

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if err := mappingKey(key, seen); err != nil {
			return err
		}

		if err := walkYAML(node.Content[i+1], depth+1); err != nil {
			return err
		}
	}

	return nil
}

// mappingKey rejects a non-string, merge, or repeated key.
func mappingKey(key *yaml.Node, seen map[string]bool) error {
	if key == nil || key.Kind != yaml.ScalarNode || key.ShortTag() != yamlTagString {
		return errWorkspaceField
	}

	if key.Value == "<<" {
		return errWorkspaceAnchor
	}

	if seen[key.Value] {
		return fmt.Errorf("%w: duplicate %q line %d", errWorkspaceField, key.Value, key.Line)
	}

	seen[key.Value] = true

	return nil
}

// standardTag reports a plain YAML type. Custom tags are rejected.
func standardTag(node *yaml.Node) bool {
	if node.Kind == yaml.DocumentNode {
		return true
	}

	switch node.ShortTag() {
	case yamlTagString, yamlTagInt, "!!float", "!!bool", "!!null", "!!map", "!!seq":
		return true
	default:
		return false
	}
}

// decodeRoot reads manifest and the optional release-align block.
func decodeRoot(root *yaml.Node) (*WorkspaceSpec, error) {
	if root == nil || root.Kind != yaml.MappingNode {
		return nil, errWorkspaceYAML
	}

	manifest, ok := mappingValue(root, keyManifest)
	if !ok || manifest.Kind != yaml.MappingNode {
		return nil, errWorkspaceSchema
	}

	if err := rejectImport(manifest); err != nil {
		return nil, err
	}

	spec := &WorkspaceSpec{
		SchemaVersion: 1,
	}
	if err := decodeManifest(spec, manifest); err != nil {
		return nil, err
	}

	if err := decodeReleaseAlign(spec, root); err != nil {
		return nil, err
	}

	return spec, nil
}

// decodeManifest reads the supported west manifest fields.
func decodeManifest(spec *WorkspaceSpec, manifest *yaml.Node) error {
	allowed := []string{"version", "defaults", "remotes", keyProjects, "group-filter", "self"}
	if err := unknownKeys(manifest, keyManifest, allowed); err != nil {
		return err
	}

	if err := rejectManifestVersion(manifest); err != nil {
		return err
	}

	bases, err := decodeRemotes(manifest)
	if err != nil {
		return err
	}

	defaultRemote, err := decodeDefaults(spec, manifest, bases)
	if err != nil {
		return err
	}

	if err = decodeProjects(spec, manifest, bases, defaultRemote); err != nil {
		return err
	}

	if err = decodeGroupFilter(spec, manifest); err != nil {
		return err
	}

	return decodeSelf(manifest)
}

// rejectManifestVersion refuses the old west 0.9 group-filter behavior.
func rejectManifestVersion(manifest *yaml.Node) error {
	node, ok := mappingValue(manifest, "version")
	if !ok {
		return nil
	}

	value, err := scalarString(node)
	if err != nil {
		return err
	}

	if value == "0.9" || value == "0.9.0" {
		return fmt.Errorf("%w: manifest.version %s", errWorkspaceField, value)
	}

	return nil
}

// decodeRemotes collects url-base values. An empty list is allowed.
func decodeRemotes(manifest *yaml.Node) (map[string]string, error) {
	node, ok := mappingValue(manifest, "remotes")
	bases := make(map[string]string)

	if !ok {
		return bases, nil
	}

	if node.Kind != yaml.SequenceNode {
		return nil, errWorkspaceField
	}

	for _, remote := range node.Content {
		name, base, err := oneRemote(remote)
		if err != nil {
			return nil, err
		}

		if bases[name] != "" {
			return nil, fmt.Errorf("%w: remote %s", errWorkspaceField, name)
		}

		bases[name] = base
	}

	return bases, nil
}

// oneRemote reads one remotes entry.
func oneRemote(node *yaml.Node) (string, string, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return "", "", errWorkspaceField
	}

	if err := unknownKeys(node, "remotes", []string{keyName, "url-base"}); err != nil {
		return "", "", err
	}

	nameNode, ok := mappingValue(node, keyName)
	if !ok {
		return "", "", errWorkspaceField
	}

	name, err := scalarString(nameNode)
	if err != nil || name == "" {
		return "", "", errWorkspaceField
	}

	baseNode, ok := mappingValue(node, "url-base")
	if !ok {
		return "", "", errWorkspaceField
	}

	base, err := scalarString(baseNode)
	if err != nil || base == "" {
		return "", "", errWorkspaceField
	}

	return name, base, nil
}

// decodeDefaults reads the inherited remote and revision.
func decodeDefaults(spec *WorkspaceSpec, manifest *yaml.Node, bases map[string]string) (string, error) {
	node, ok := mappingValue(manifest, "defaults")
	if !ok {
		spec.implicitMaster = true

		return "", nil
	}

	if node.Kind != yaml.MappingNode {
		return "", errWorkspaceField
	}

	if err := unknownKeys(node, "defaults", []string{keyRemote, keyRevision}); err != nil {
		return "", err
	}

	remote, err := optionalString(node, keyRemote)
	if err != nil {
		return "", err
	}

	if remote != "" && bases[remote] == "" {
		return "", fmt.Errorf("%w: defaults.remote %s", errWorkspaceRemote, remote)
	}

	if err = decodeDefaultRevision(spec, node); err != nil {
		return "", err
	}

	return remote, nil
}

// decodeDefaultRevision stores a typed revision or a short name. Omission means master.
func decodeDefaultRevision(spec *WorkspaceSpec, defaults *yaml.Node) error {
	node, ok := mappingValue(defaults, keyRevision)
	if !ok {
		spec.implicitMaster = true

		return nil
	}

	revision, short, err := parseRevision(node)
	if err != nil {
		return err
	}

	spec.DefaultRevision = revision
	spec.shortDefault = short

	return nil
}

// decodeProjects reads every project and resolves explicit URLs.
func decodeProjects(
	spec *WorkspaceSpec,
	manifest *yaml.Node,
	bases map[string]string,
	defaultRemote string,
) error {
	node, ok := mappingValue(manifest, keyProjects)
	if !ok || node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return errWorkspaceSchema
	}

	spec.Projects = make([]*ProjectSpec, 0, len(node.Content))

	for _, item := range node.Content {
		project, err := decodeProject(item, bases, defaultRemote)
		if err != nil {
			return err
		}

		spec.Projects = append(spec.Projects, project)
	}

	return nil
}

// decodeProject reads one west project. path falls back to name.
func decodeProject(node *yaml.Node, bases map[string]string, defaultRemote string) (*ProjectSpec, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, errWorkspaceField
	}

	if err := rejectProjectKeys(node); err != nil {
		return nil, err
	}

	name, path, err := projectIdentity(node)
	if err != nil {
		return nil, err
	}

	cloneURL, err := projectCloneURL(node, name, bases, defaultRemote)
	if err != nil {
		return nil, err
	}

	groups, err := optionalStringList(node, keyGroups)
	if err != nil {
		return nil, err
	}

	revision, short, err := optionalRevision(node)
	if err != nil {
		return nil, err
	}

	depth, hasDepth, err := decodeCloneDepth(node)
	if err != nil {
		return nil, err
	}

	project := &ProjectSpec{
		Name:          name,
		Path:          path,
		URL:           cloneURL,
		Groups:        groups,
		Revision:      revision,
		shortRevision: short,
		CloneDepth:    depthPointer(depth, hasDepth),
	}

	return project, nil
}

// rejectProjectKeys allows known metadata and rejects import and submodules.
func rejectProjectKeys(node *yaml.Node) error {
	if err := rejectImport(node); err != nil {
		return err
	}

	if err := rejectSubmodules(node); err != nil {
		return err
	}

	allowed := []string{
		keyName, keyRemote, keyURL, "repo-path", keyPath, keyRevision, keyGroups,
		keyDescription, keyUserdata, "clone-depth", keyWestCommands,
	}

	return unknownKeys(node, "projects", allowed)
}

// projectIdentity reads name and path. A missing path uses the name.
func projectIdentity(node *yaml.Node) (string, string, error) {
	nameNode, ok := mappingValue(node, keyName)
	if !ok {
		return "", "", errWorkspaceName
	}

	name, err := scalarString(nameNode)
	if err != nil || name == "" || strings.ContainsAny(name, `/\`) || name == reservedProjectName {
		return "", "", errWorkspaceName
	}

	path, err := optionalString(node, keyPath)
	if err != nil {
		return "", "", err
	}

	if path == "" {
		path = name
	}

	return name, path, nil
}

// projectCloneURL follows west: explicit url, otherwise url-base plus repo-path or name.
// The join does not add .git and does not trim a slash from url-base.
func projectCloneURL(node *yaml.Node, name string, bases map[string]string, defaultRemote string) (string, error) {
	_, hasURL := mappingValue(node, keyURL)
	_, hasRemote := mappingValue(node, keyRemote)
	_, hasRepo := mappingValue(node, "repo-path")

	if hasURL && (hasRemote || hasRepo) {
		return "", errWorkspaceRemote
	}

	if hasURL {
		return optionalString(node, keyURL)
	}

	remote := defaultRemote

	if hasRemote {
		value, err := optionalString(node, keyRemote)
		if err != nil {
			return "", err
		}

		remote = value
	}

	if remote == "" {
		return "", nil
	}

	base, ok := bases[remote]
	if !ok {
		return "", fmt.Errorf("%w: remote %s", errWorkspaceRemote, remote)
	}

	repo := name

	if hasRepo {
		value, err := optionalString(node, "repo-path")
		if err != nil {
			return "", err
		}

		repo = value
	}

	return base + "/" + repo, nil
}

// optionalRevision reads a project revision. Absence inherits the default.
func optionalRevision(node *yaml.Node) (*RevisionSpec, string, error) {
	value, ok := mappingValue(node, keyRevision)
	if !ok {
		return nil, "", nil
	}

	return parseRevision(value)
}

// parseRevision classifies refs/heads, refs/tags, a full id, or a short name.
func parseRevision(node *yaml.Node) (*RevisionSpec, string, error) {
	if node == nil || node.Kind != yaml.ScalarNode || node.ShortTag() != yamlTagString {
		line := 0
		if node != nil {
			line = node.Line
		}

		return nil, "", fmt.Errorf("%w: line %d", errWorkspaceRevisionType, line)
	}

	value := node.Value
	switch {
	case strings.HasPrefix(value, refHeads):
		revision := &RevisionSpec{
			Branch: strings.TrimPrefix(value, refHeads),
		}
		if err := revision.Validate(); err != nil {
			return nil, "", err
		}

		return revision, "", nil
	case strings.HasPrefix(value, refTags):
		revision := &RevisionSpec{
			Tag: strings.TrimPrefix(value, refTags),
		}
		if err := revision.Validate(); err != nil {
			return nil, "", err
		}

		return revision, "", nil
	case workspaceOID.MatchString(value):
		return &RevisionSpec{
			Commit: value,
		}, "", nil
	default:
		if revisionExpression(value) {
			return nil, "", fmt.Errorf("%w: line %d", errWorkspaceRevisionShort, node.Line)
		}

		return nil, value, nil
	}
}

// revisionExpression reports a west expression this reader does not classify locally.
func revisionExpression(value string) bool {
	return value == "" || strings.HasPrefix(value, "-") || strings.Contains(value, "..") ||
		strings.Contains(value, "@{") || strings.ContainsAny(value, " \t~^:")
}

// decodeCloneDepth reads a positive integer hint. The boolean reports that the key was present.
func decodeCloneDepth(node *yaml.Node) (int, bool, error) {
	raw, ok := mappingValue(node, "clone-depth")
	if !ok {
		return 0, false, nil
	}

	if raw.ShortTag() != yamlTagInt {
		return 0, false, fmt.Errorf("%w: clone-depth line %d", errWorkspaceField, raw.Line)
	}

	depth, err := strconv.Atoi(raw.Value)
	if err != nil || depth <= 0 {
		return 0, false, fmt.Errorf("%w: clone-depth line %d", errWorkspaceField, raw.Line)
	}

	return depth, true, nil
}

// depthPointer stores a present clone-depth.
func depthPointer(depth int, present bool) *int {
	if !present {
		return nil
	}

	value := depth

	return &value
}

// decodeGroupFilter reads the ordered enable and disable list.
func decodeGroupFilter(spec *WorkspaceSpec, manifest *yaml.Node) error {
	node, ok := mappingValue(manifest, "group-filter")
	if !ok {
		return nil
	}

	if node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return fmt.Errorf("%w: group-filter", errWorkspaceField)
	}

	spec.GroupFilter = make([]*GroupFilter, 0, len(node.Content))

	for _, item := range node.Content {
		filter, err := oneGroupFilter(item)
		if err != nil {
			return err
		}

		spec.GroupFilter = append(spec.GroupFilter, filter)
	}

	return nil
}

// oneGroupFilter requires a leading + or -.
func oneGroupFilter(node *yaml.Node) (*GroupFilter, error) {
	raw, err := scalarString(node)
	if err != nil || raw == "" || (raw[0] != '+' && raw[0] != '-') {
		return nil, fmt.Errorf("%w: group-filter", errWorkspaceField)
	}

	name := raw[1:]
	if name == "" || strings.ContainsAny(name, " \t,:") {
		return nil, fmt.Errorf("%w: group-filter %s", errWorkspaceField, raw)
	}

	filter := &GroupFilter{
		Name:    name,
		Disable: raw[0] == '-',
	}

	return filter, nil
}

// decodeSelf rejects import and otherwise leaves self data in the node.
func decodeSelf(manifest *yaml.Node) error {
	node, ok := mappingValue(manifest, "self")
	if !ok {
		return nil
	}

	if node.Kind != yaml.MappingNode {
		return errWorkspaceField
	}

	if err := rejectImport(node); err != nil {
		return err
	}

	return unknownKeys(node, "self", []string{keyPath, keyWestCommands, keyUserdata, keyDescription})
}

// rejectImport fails before any Git command when a manifest imports another file.
func rejectImport(node *yaml.Node) error {
	if _, ok := mappingValue(node, "import"); ok {
		return errWorkspaceImport
	}

	return nil
}

// rejectSubmodules allows an omitted value or false and rejects every other form.
func rejectSubmodules(node *yaml.Node) error {
	value, ok := mappingValue(node, "submodules")
	if !ok || (value.Kind == yaml.ScalarNode && value.Value == "false") {
		return nil
	}

	return errWorkspaceSubmodules
}

// decodeReleaseAlign reads our block. Its absence is valid for a plain manifest.
func decodeReleaseAlign(spec *WorkspaceSpec, root *yaml.Node) error {
	node, ok := mappingValue(root, keyReleaseAlign)
	if !ok {
		return nil
	}

	if node.Kind != yaml.MappingNode {
		return errWorkspaceField
	}

	allowed := []string{"schema-version", "release", "gitlab", "timeouts"}
	if err := unknownKeys(node, keyReleaseAlign, allowed); err != nil {
		return err
	}

	version, ok := mappingValue(node, "schema-version")
	if !ok || !scalarIsOne(version) {
		return errWorkspaceSchema
	}

	release, err := optionalString(node, "release")
	if err != nil {
		return err
	}

	spec.Release = release
	if err = decodeGitLab(spec, node); err != nil {
		return err
	}

	return decodeTimeouts(spec, node)
}

// decodeGitLab reads the discovery scope. It is not derived from remotes.
func decodeGitLab(spec *WorkspaceSpec, block *yaml.Node) error {
	node, ok := mappingValue(block, "gitlab")
	if !ok {
		return nil
	}

	if node.Kind != yaml.MappingNode {
		return errWorkspaceField
	}

	if err := unknownKeys(node, "gitlab", []string{keyURL, keyGroups, "clone-protocol"}); err != nil {
		return err
	}

	rawURL, err := optionalString(node, keyURL)
	if err != nil {
		return err
	}

	groups, err := optionalStringList(node, keyGroups)
	if err != nil {
		return err
	}

	protocol, err := optionalString(node, "clone-protocol")
	if err != nil {
		return err
	}

	spec.GitLab = &GitLabSource{
		URL:           rawURL,
		Groups:        groups,
		CloneProtocol: protocol,
	}

	return nil
}

// decodeTimeouts reads only the duration overrides that are present.
func decodeTimeouts(spec *WorkspaceSpec, block *yaml.Node) error {
	node, ok := mappingValue(block, "timeouts")
	if !ok {
		return nil
	}

	if node.Kind != yaml.MappingNode {
		return errWorkspaceField
	}

	names := []string{timeoutProbe, timeoutFetch, timeoutLocal, timeoutClone, timeoutArchive}
	if err := unknownKeys(node, "timeouts", names); err != nil {
		return err
	}

	timeouts := new(WorkspaceTimeouts)

	var err error

	if err = assignTimeout(node, timeoutProbe, &timeouts.Probe); err != nil {
		return err
	}

	if err = assignTimeout(node, timeoutFetch, &timeouts.Fetch); err != nil {
		return err
	}

	if err = assignTimeout(node, timeoutLocal, &timeouts.Local); err != nil {
		return err
	}

	if err = assignTimeout(node, timeoutClone, &timeouts.Clone); err != nil {
		return err
	}

	if err = assignTimeout(node, timeoutArchive, &timeouts.Archive); err != nil {
		return err
	}

	spec.Timeouts = timeouts

	return nil
}

// assignTimeout copies one scalar into dest. A missing key leaves dest nil.
func assignTimeout(node *yaml.Node, key string, dest **string) error {
	value, ok := mappingValue(node, key)
	if !ok {
		return nil
	}

	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("%w: %s line %d", errWorkspaceField, key, value.Line)
	}

	text := value.Value
	*dest = &text

	return nil
}

// unknownKeys rejects a key that this reader would otherwise drop.
func unknownKeys(node *yaml.Node, path string, allowed []string) error {
	known := make(map[string]bool, len(allowed))
	for _, key := range allowed {
		known[key] = true
	}

	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i]
		if !known[key.Value] {
			return fmt.Errorf("%w: %s.%s line %d", errWorkspaceField, path, key.Value, key.Line)
		}
	}

	return nil
}

// mappingValue returns one mapping value by its string key.
func mappingValue(node *yaml.Node, key string) (*yaml.Node, bool) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, false
	}

	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1], true
		}
	}

	return nil, false
}

// scalarString reads a string scalar and rejects numbers and booleans.
func scalarString(node *yaml.Node) (string, error) {
	if node == nil || node.Kind != yaml.ScalarNode || node.ShortTag() != yamlTagString {
		line := 0
		if node != nil {
			line = node.Line
		}

		return "", fmt.Errorf("%w: line %d", errWorkspaceField, line)
	}

	return node.Value, nil
}

// optionalString reads a string key. A missing key is empty.
func optionalString(node *yaml.Node, key string) (string, error) {
	value, ok := mappingValue(node, key)
	if !ok {
		return "", nil
	}

	return scalarString(value)
}

// optionalStringList reads a sequence of strings. A missing key is nil.
func optionalStringList(node *yaml.Node, key string) ([]string, error) {
	value, ok := mappingValue(node, key)
	if !ok {
		return nil, nil
	}

	if value.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("%w: %s", errWorkspaceField, key)
	}

	out := make([]string, 0, len(value.Content))

	for _, item := range value.Content {
		text, err := scalarString(item)
		if err != nil {
			return nil, err
		}

		out = append(out, text)
	}

	return out, nil
}

// scalarIsOne accepts a numeric 1 or the string 1.
func scalarIsOne(node *yaml.Node) bool {
	return node != nil && node.Kind == yaml.ScalarNode && node.Value == "1" &&
		(node.ShortTag() == yamlTagInt || node.ShortTag() == yamlTagString)
}

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

	name := base + "-" + hex.EncodeToString(sum[:4])
	if used[name] {
		name += "-2"
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

	return yaml.Marshal(root)
}

// encodeEdited appends projects that are not in the node and updates the release label.
func encodeEdited(root *yaml.Node, spec *WorkspaceSpec) ([]byte, error) {
	next := copyNode(root)
	setRelease(next, spec.Release)

	if err := appendMissingProjects(next, spec); err != nil {
		return nil, err
	}

	return yaml.Marshal(next)
}

// manifestNode is the west manifest written by init.
func manifestNode(spec *WorkspaceSpec) *yaml.Node {
	manifest := mappingNode()
	manifest.Content = append(manifest.Content, strNode("version", false), strNode("1.0", true))

	if spec.DefaultRevision != nil {
		defaults := mappingNode()
		text, quote := revisionText(spec.DefaultRevision)
		defaults.Content = append(defaults.Content, strNode(keyRevision, false), strNode(text, quote))
		manifest.Content = append(manifest.Content, strNode("defaults", false), defaults)
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

	next := *node
	if len(node.Content) == 0 {
		return &next
	}

	next.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		next.Content[i] = copyNode(child)
	}

	return &next
}

// mappingNode is an empty YAML mapping.
func mappingNode() *yaml.Node {
	return &yaml.Node{
		Kind: yaml.MappingNode,
	}
}

// strNode is a string scalar. quote forces double quotes for versions and object ids.
func strNode(value string, quote bool) *yaml.Node {
	node := &yaml.Node{
		Kind:  yaml.ScalarNode,
		Value: value,
	}
	if quote {
		node.Style = yaml.DoubleQuotedStyle
		node.Tag = yamlTagString
	}

	return node
}

// intNode is an integer scalar.
func intNode(value int) *yaml.Node {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   yamlTagInt,
		Value: strconv.Itoa(value),
	}
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
