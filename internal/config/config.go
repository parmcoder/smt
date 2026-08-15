// Package config loads and validates SMT workspace configuration.
package config

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config is the typed SMT configuration file.
type Config struct {
	Version      int          `yaml:"version"`
	Workspace    Workspace    `yaml:"workspace,omitempty"`
	Providers    Providers    `yaml:"providers,omitempty"`
	Commit       CommitConfig `yaml:"commit"`
	Repositories []Repository `yaml:"repositories"`
	Contracts    Contracts    `yaml:"contracts,omitempty"`
	Workflow     *Workflow    `yaml:"workflow,omitempty"`
}

// Workflow records the fixed Codex delivery roles and plugins for a generated
// blueprint. It is optional so existing version 1 configurations remain valid.
type Workflow struct {
	Policy  WorkflowPolicy   `yaml:"policy"`
	Plugins []WorkflowPlugin `yaml:"plugins"`
}

// WorkflowPolicy assigns the fixed delivery responsibilities.
type WorkflowPolicy struct {
	Manager        string `yaml:"manager"`
	Implementation string `yaml:"implementation"`
	Documentation  string `yaml:"documentation"`
	ReviewRequired bool   `yaml:"review_required"`
}

// WorkflowPlugin is one required plugin source and its ordered selectors.
type WorkflowPlugin struct {
	Source    string   `yaml:"source"`
	Selectors []string `yaml:"selectors"`
}

// Workspace records the initialized platform choices without prescribing
// dependency versions or application implementation details.
type Workspace struct {
	AIAssist string         `yaml:"ai_assist,omitempty"`
	Stack    WorkspaceStack `yaml:"stack,omitempty"`
}

// WorkspaceStack contains the fixed first-release component profiles.
type WorkspaceStack struct {
	Web      string   `yaml:"web,omitempty"`
	Mobile   string   `yaml:"mobile,omitempty"`
	API      string   `yaml:"api,omitempty"`
	Database string   `yaml:"database,omitempty"`
	DevOps   []string `yaml:"devops,omitempty"`
}

// Providers contains optional provider settings.
type Providers struct {
	GitLab ProviderConfig `yaml:"gitlab"`
	GitHub ProviderConfig `yaml:"github"`
}

// ProviderConfig contains provider endpoint settings.
type ProviderConfig struct {
	APIBaseURL          string `yaml:"api_base_url"`
	EnterpriseBaseURL   string `yaml:"enterprise_base_url"`
	EnterpriseUploadURL string `yaml:"enterprise_upload_url"`
}

// CommitConfig defines the commit types and scopes accepted by SMT.
type CommitConfig struct {
	Types  []string `yaml:"types"`
	Scopes []string `yaml:"scopes"`
}

// Repository describes one root repository or independent submodule.
type Repository struct {
	ID            string        `yaml:"id"`
	Path          string        `yaml:"path"`
	Component     string        `yaml:"component,omitempty"`
	Technology    string        `yaml:"technology,omitempty"`
	Remote        Remote        `yaml:"remote"`
	Provider      string        `yaml:"provider,omitempty"`
	Project       string        `yaml:"project,omitempty"`
	Visibility    string        `yaml:"visibility,omitempty"`
	Scope         string        `yaml:"scope"`
	Checks        []Check       `yaml:"-"`
	Profiles      CheckProfiles `yaml:"-"`
	HasChecks     bool          `yaml:"-"`
	UnknownFields []string      `yaml:"-"`
}

// Remote holds a credential-free Git destination configured after init.
type Remote struct {
	URL           string `yaml:"url"`
	DefaultBranch string `yaml:"default_branch,omitempty"`
}

// EffectiveDefaultBranch returns the configured base branch, using the stable
// version-1 default when it is absent or blank.
func (r Remote) EffectiveDefaultBranch() string {
	if branch := strings.TrimSpace(r.DefaultBranch); branch != "" {
		return branch
	}
	return "main"
}

// EffectiveDefaultBranch returns the repository's configured base branch.
func (r Repository) EffectiveDefaultBranch() string { return r.Remote.EffectiveDefaultBranch() }

// Check is a configured preflight check.
type Check struct {
	Kind            string   `yaml:"kind"`
	Argv            []string `yaml:"argv"`
	Include         []string `yaml:"include"`
	MutatesWorktree bool     `yaml:"mutates_worktree"`
}

// CheckProfiles contains checks grouped by the stage that invokes them.
type CheckProfiles map[string][]Check

// Contracts contains reusable repository contract declarations.
type Contracts struct {
	Reference         []Contract `yaml:"reference"`
	MigrationCoverage []Contract `yaml:"migration-coverage"`
	Artifact          []Contract `yaml:"artifact"`
}

// Contract describes one expected repository file relationship or artifact.
type Contract struct {
	ID          string `yaml:"id"`
	Repository  string `yaml:"repository"`
	File        string `yaml:"file"`
	Expected    string `yaml:"expected"`
	Replacement string `yaml:"replacement"`
	Source      string `yaml:"source"`
	Severity    string `yaml:"severity"`
}

// UnmarshalYAML accepts both the legacy check list and named check profiles.
func (r *Repository) UnmarshalYAML(value *yaml.Node) error {
	type repositoryFields struct {
		ID         string    `yaml:"id"`
		Path       string    `yaml:"path"`
		Component  string    `yaml:"component"`
		Technology string    `yaml:"technology"`
		Remote     Remote    `yaml:"remote"`
		Provider   string    `yaml:"provider"`
		Project    string    `yaml:"project"`
		Visibility string    `yaml:"visibility"`
		Scope      string    `yaml:"scope"`
		Checks     yaml.Node `yaml:"checks"`
	}
	var raw repositoryFields
	if err := value.Decode(&raw); err != nil {
		return err
	}
	r.ID, r.Path = raw.ID, raw.Path
	r.Component, r.Technology, r.Remote = raw.Component, raw.Technology, raw.Remote
	r.Provider, r.Project, r.Visibility, r.Scope = raw.Provider, raw.Project, raw.Visibility, raw.Scope
	allowed := map[string]bool{"id": true, "path": true, "component": true, "technology": true, "remote": true, "provider": true, "project": true, "visibility": true, "scope": true, "checks": true}
	for i := 0; i+1 < len(value.Content); i += 2 {
		if !allowed[value.Content[i].Value] {
			r.UnknownFields = append(r.UnknownFields, value.Content[i].Value)
		}
	}
	if raw.Checks.Kind == 0 {
		return nil
	}
	r.HasChecks = true
	switch raw.Checks.Kind {
	case yaml.SequenceNode:
		return raw.Checks.Decode(&r.Checks)
	case yaml.MappingNode:
		return raw.Checks.Decode(&r.Profiles)
	default:
		return fmt.Errorf("repository %q checks must be a list or profile map", r.ID)
	}
}

// Load decodes and validates an SMT YAML file.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	return LoadBytes(raw, path)
}

// LoadBytes decodes and validates exact configuration bytes using sourcePath
// only to establish the workspace root for path validation.
func LoadBytes(raw []byte, sourcePath string) (*Config, error) {
	var cfg Config
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("config must contain one YAML document")
		}
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := cfg.Validate(filepath.Dir(sourcePath)); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate checks structural rules that do not require inspecting Git.
func (c *Config) Validate(workspaceRoot string) error {
	if c.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if len(c.Commit.Types) == 0 {
		return fmt.Errorf("commit.types must not be empty")
	}
	if len(c.Commit.Scopes) == 0 {
		return fmt.Errorf("commit.scopes must not be empty")
	}
	commitScopes := make(map[string]struct{}, len(c.Commit.Scopes))
	for _, scope := range c.Commit.Scopes {
		if scope == "" {
			return fmt.Errorf("commit scope must not be empty")
		}
		if _, exists := commitScopes[scope]; exists {
			return fmt.Errorf("duplicate commit scope %q", scope)
		}
		commitScopes[scope] = struct{}{}
	}
	if err := c.Workspace.validate(); err != nil {
		return err
	}
	if err := c.validateMobile(); err != nil {
		return err
	}
	if err := c.validateWorkflow(); err != nil {
		return err
	}

	seen := map[string]map[string]struct{}{
		"id": {}, "path": {}, "project": {}, "scope": {},
	}
	rootCount := 0
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return fmt.Errorf("resolve workspace root: %w", err)
	}
	for i, repo := range c.Repositories {
		normalizedPath := filepath.Clean(repo.Path)
		for field, value := range map[string]string{
			"id": repo.ID, "path": normalizedPath, "scope": repo.Scope,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("repository %d %s is required", i, field)
			}
			if _, exists := seen[field][value]; exists {
				return fmt.Errorf("duplicate repository %s %q", field, value)
			}
			seen[field][value] = struct{}{}
		}
		if repo.Project != "" {
			if _, exists := seen["project"][repo.Project]; exists {
				return fmt.Errorf("duplicate repository project %q", repo.Project)
			}
			seen["project"][repo.Project] = struct{}{}
		}
		if repo.Provider != "" && repo.Provider != "gitlab" && repo.Provider != "github" {
			return fmt.Errorf("provider must be gitlab or github")
		}
		if repo.Provider == "" && repo.Project != "" {
			return fmt.Errorf("repository %q project requires provider", repo.ID)
		}
		if repo.Provider != "" && repo.Project == "" {
			return fmt.Errorf("repository %q provider requires project", repo.ID)
		}
		if repo.Visibility != "" && repo.Visibility != "private" && repo.Visibility != "public" {
			return fmt.Errorf("repository %q visibility must be private or public", repo.ID)
		}
		if repo.Visibility != "" && repo.Provider == "" {
			return fmt.Errorf("repository %q visibility requires provider", repo.ID)
		}
		if repo.Provider != "" {
			if err := validateProjectIdentity(repo.Provider, repo.Project); err != nil {
				return fmt.Errorf("repository %q: %w", repo.ID, err)
			}
		}
		if err := validateComponent(repo); err != nil {
			return fmt.Errorf("repository %q: %w", repo.ID, err)
		}
		if err := validateRemoteURL(repo.Remote.URL); err != nil {
			return fmt.Errorf("repository %q: %w", repo.ID, err)
		}
		if normalizedPath == "." {
			rootCount++
		}
		resolved := filepath.Clean(filepath.Join(root, normalizedPath))
		if resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator)) {
			return fmt.Errorf("repository path %q must remain inside workspace", repo.Path)
		}
		if _, ok := commitScopes[repo.Scope]; !ok {
			return fmt.Errorf("scope %q is not declared in commit.scopes", repo.Scope)
		}
		for profile, checks := range repo.profiles() {
			if profile != "legacy" && profile != "hook" && profile != "submit" && profile != "ci-parity" {
				return fmt.Errorf("repository %q has unknown check profile %q", repo.ID, profile)
			}
			for _, check := range checks {
				if err := validateCheck(check); err != nil {
					return fmt.Errorf("repository %q %w", repo.ID, err)
				}
			}
		}
	}
	if rootCount != 1 {
		return fmt.Errorf("exactly one root repository with path . is required")
	}
	return c.validateContracts(root, seenRepositoryIDs(c.Repositories))
}

// EffectiveVisibility returns the provisioning default for a configured provider project.
func (r Repository) EffectiveVisibility() string {
	if r.Visibility == "" {
		return "private"
	}
	return r.Visibility
}

var projectComponentPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validateProjectIdentity(provider, project string) error {
	if strings.Contains(project, "://") || strings.TrimSpace(project) != project {
		return fmt.Errorf("project must be a fully qualified provider path")
	}
	parts := strings.Split(project, "/")
	minimum := 2
	if provider == "github" && len(parts) != 2 {
		return fmt.Errorf("github project must use owner/repository")
	}
	if len(parts) < minimum {
		return fmt.Errorf("%s project must include a namespace and repository", provider)
	}
	for _, part := range parts {
		if !projectComponentPattern.MatchString(part) {
			return fmt.Errorf("project must contain only qualified path components")
		}
	}
	return nil
}

// MarshalYAML preserves the legacy checks list and named check profiles that
// are intentionally kept out of the runtime-only Repository fields. Commands
// that update remote URLs must not silently erase local readiness checks.
func (r Repository) MarshalYAML() (interface{}, error) {
	type repositoryFields struct {
		ID         string `yaml:"id"`
		Path       string `yaml:"path"`
		Component  string `yaml:"component,omitempty"`
		Technology string `yaml:"technology,omitempty"`
		Remote     Remote `yaml:"remote"`
		Provider   string `yaml:"provider,omitempty"`
		Project    string `yaml:"project,omitempty"`
		Visibility string `yaml:"visibility,omitempty"`
		Scope      string `yaml:"scope"`
		Checks     any    `yaml:"checks,omitempty"`
	}
	var checks any
	if r.Profiles != nil {
		checks = r.Profiles
	} else if r.HasChecks || len(r.Checks) > 0 {
		checks = r.Checks
	}
	return repositoryFields{
		ID: r.ID, Path: r.Path, Component: r.Component, Technology: r.Technology,
		Remote: r.Remote, Provider: r.Provider, Project: r.Project,
		Visibility: r.Visibility, Scope: r.Scope, Checks: checks,
	}, nil
}

func (c *Config) validateWorkflow() error {
	if c.Workflow == nil {
		return nil
	}
	w := c.Workflow
	if w.Policy.Manager != "work_manager" || w.Policy.Implementation != "backend_worker" || w.Policy.Documentation != "doc_writer" || !w.Policy.ReviewRequired {
		return fmt.Errorf("workflow.policy must use the fixed work_manager, backend_worker, doc_writer, and review_required values")
	}
	want := []WorkflowPlugin{
		{Source: "parmcoder/codex-obsidian", Selectors: []string{"codex-obsidian-writer", "codex-obsidian-markdown"}},
		{Source: "parmcoder/godex", Selectors: []string{"godex-go-backend"}},
	}
	if len(w.Plugins) != len(want) {
		return fmt.Errorf("workflow.plugins must contain the two fixed plugins in order")
	}
	for i := range want {
		if w.Plugins[i].Source != want[i].Source || len(w.Plugins[i].Selectors) != len(want[i].Selectors) {
			return fmt.Errorf("workflow plugin %d does not match the required source and selectors", i)
		}
		for j := range want[i].Selectors {
			if w.Plugins[i].Selectors[j] != want[i].Selectors[j] {
				return fmt.Errorf("workflow plugin %d does not match the required source and selectors", i)
			}
		}
	}
	return nil
}

func (w Workspace) validate() error {
	if w.AIAssist != "" && w.AIAssist != "codex" {
		return fmt.Errorf("workspace.ai_assist must be codex when set")
	}
	for _, stack := range []struct {
		field string
		value string
		want  string
	}{
		{field: "web", value: w.Stack.Web, want: "nextjs"},
		{field: "mobile", value: w.Stack.Mobile, want: "flutter"},
		{field: "api", value: w.Stack.API, want: "go"},
		{field: "database", value: w.Stack.Database, want: "postgresql"},
	} {
		if stack.value != "" && stack.value != stack.want {
			return fmt.Errorf("workspace.stack.%s must be %s when set", stack.field, stack.want)
		}
	}
	seen := make(map[string]struct{}, len(w.Stack.DevOps))
	for _, tool := range w.Stack.DevOps {
		if tool != "docker" && tool != "opentofu" {
			return fmt.Errorf("workspace.stack.devops contains unsupported tool %q", tool)
		}
		if _, ok := seen[tool]; ok {
			return fmt.Errorf("workspace.stack.devops contains duplicate tool %q", tool)
		}
		seen[tool] = struct{}{}
	}
	return nil
}

func (c Config) validateMobile() error {
	const (
		mobileID         = "mobile"
		mobilePath       = "mobile-app"
		mobileComponent  = "mobile"
		mobileTechnology = "flutter"
		mobileScope      = "mobile"
	)
	if c.Workspace.Stack.Mobile == "" {
		for _, repository := range c.Repositories {
			if repository.Component == mobileComponent {
				return fmt.Errorf("mobile repository requires workspace.stack.mobile")
			}
		}
		return nil
	}
	for _, repository := range c.Repositories {
		if repository.ID != mobileID {
			continue
		}
		if repository.Path == mobilePath && repository.Component == mobileComponent && repository.Technology == mobileTechnology && repository.Scope == mobileScope {
			return nil
		}
		return fmt.Errorf("mobile repository must use id=%q, path=%q, component=%q, technology=%q, and scope=%q", mobileID, mobilePath, mobileComponent, mobileTechnology, mobileScope)
	}
	return fmt.Errorf("mobile repository must use id=%q, path=%q, component=%q, technology=%q, and scope=%q", mobileID, mobilePath, mobileComponent, mobileTechnology, mobileScope)
}

func validateComponent(repository Repository) error {
	if repository.Component == "" && repository.Technology == "" {
		return nil
	}
	expected := map[string]string{
		"web":      "nextjs",
		"mobile":   "flutter",
		"api":      "go",
		"database": "postgresql",
		"devops":   "docker-opentofu",
	}
	want, ok := expected[repository.Component]
	if !ok {
		return fmt.Errorf("component must be web, mobile, api, database, or devops")
	}
	if repository.Technology != want {
		return fmt.Errorf("technology for component %q must be %q", repository.Component, want)
	}
	return nil
}

func validateRemoteURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if !strings.Contains(raw, "://") {
		// Git also accepts scp-like SSH URLs such as git@host:group/repo.git.
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("remote.url is invalid: %w", err)
	}
	if parsed.User != nil && parsed.Scheme != "ssh" {
		return fmt.Errorf("remote.url must not contain credentials")
	}
	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return fmt.Errorf("remote.url must not contain credentials")
		}
	}
	for _, key := range []string{"access_token", "token", "password"} {
		if parsed.Query().Get(key) != "" {
			return fmt.Errorf("remote.url must not contain credentials")
		}
	}
	return nil
}

func (r Repository) profiles() CheckProfiles {
	if r.Profiles != nil {
		return r.Profiles
	}
	return CheckProfiles{"legacy": r.Checks}
}

func validateCheck(check Check) error {
	if len(check.Argv) == 0 {
		return fmt.Errorf("%s check argv must not be empty", check.Kind)
	}
	for _, arg := range check.Argv {
		if arg == "" {
			return fmt.Errorf("check argv contains an empty argument")
		}
	}
	switch check.Kind {
	case "command":
	case "sql-format":
		if len(check.Argv) != 1 || check.Argv[0] != "pg_format" {
			return fmt.Errorf("sql-format check argv must be [pg_format]")
		}
		if len(check.Include) == 0 {
			return fmt.Errorf("sql-format check include must not be empty")
		}
		for _, pattern := range check.Include {
			if pattern == "" {
				return fmt.Errorf("sql-format check include contains an empty pattern")
			}
			if _, err := path.Match(pattern, "probe.sql"); err != nil {
				return fmt.Errorf("sql-format check include pattern %q is invalid: %w", pattern, err)
			}
		}
	default:
		return fmt.Errorf("unknown check kind %q", check.Kind)
	}
	return nil
}

var contractIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func seenRepositoryIDs(repositories []Repository) map[string]struct{} {
	ids := make(map[string]struct{}, len(repositories))
	for _, repository := range repositories {
		ids[repository.ID] = struct{}{}
	}
	return ids
}

func (c *Config) validateContracts(root string, repositories map[string]struct{}) error {
	seen := map[string]struct{}{}
	groups := []struct {
		name    string
		items   []Contract
		require string
	}{{"reference", c.Contracts.Reference, "replacement"}, {"migration-coverage", c.Contracts.MigrationCoverage, "source"}, {"artifact", c.Contracts.Artifact, ""}}
	for _, group := range groups {
		for i, contract := range group.items {
			if !contractIDPattern.MatchString(contract.ID) {
				return fmt.Errorf("%s contract %d id is invalid", group.name, i)
			}
			if _, ok := seen[contract.ID]; ok {
				return fmt.Errorf("duplicate contract id %q", contract.ID)
			}
			seen[contract.ID] = struct{}{}
			if _, ok := repositories[contract.Repository]; !ok {
				return fmt.Errorf("contract repository %q for %s contract %q is not configured", contract.Repository, group.name, contract.ID)
			}
			if err := validateContractFile(root, contract.File); err != nil {
				return fmt.Errorf("contract file for %s contract %q: %w", group.name, contract.ID, err)
			}
			if strings.TrimSpace(contract.Expected) == "" {
				return fmt.Errorf("%s contract %q expected is required", group.name, contract.ID)
			}
			if group.require != "" && strings.TrimSpace(fieldValue(contract, group.require)) == "" {
				return fmt.Errorf("%s contract %q %s is required", group.name, contract.ID, group.require)
			}
			if contract.Severity == "" {
				continue
			}
			if contract.Severity != "error" && contract.Severity != "warn" {
				return fmt.Errorf("%s contract %q severity must be error or warn", group.name, contract.ID)
			}
		}
	}
	for i := range c.Contracts.Reference {
		if c.Contracts.Reference[i].Severity == "" {
			c.Contracts.Reference[i].Severity = "error"
		}
	}
	for i := range c.Contracts.MigrationCoverage {
		if c.Contracts.MigrationCoverage[i].Severity == "" {
			c.Contracts.MigrationCoverage[i].Severity = "error"
		}
	}
	for i := range c.Contracts.Artifact {
		if c.Contracts.Artifact[i].Severity == "" {
			c.Contracts.Artifact[i].Severity = "error"
		}
	}
	return nil
}

func fieldValue(contract Contract, field string) string {
	if field == "replacement" {
		return contract.Replacement
	}
	return contract.Source
}

func validateContractFile(root, file string) error {
	if strings.TrimSpace(file) == "" || filepath.IsAbs(file) {
		return fmt.Errorf("must be a non-empty workspace-relative path")
	}
	clean := filepath.Clean(file)
	resolved := filepath.Clean(filepath.Join(root, clean))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || (resolved != root && !strings.HasPrefix(resolved, root+string(filepath.Separator))) {
		return fmt.Errorf("must remain inside workspace")
	}
	return nil
}
