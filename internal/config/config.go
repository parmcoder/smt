// Package config loads and validates SMT workspace configuration.
package config

import (
	"fmt"
	"io"
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
	Providers    Providers    `yaml:"providers"`
	Commit       CommitConfig `yaml:"commit"`
	Repositories []Repository `yaml:"repositories"`
	Contracts    Contracts    `yaml:"contracts"`
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
	ID       string        `yaml:"id"`
	Path     string        `yaml:"path"`
	Provider string        `yaml:"provider"`
	Project  string        `yaml:"project"`
	Scope    string        `yaml:"scope"`
	Checks   []Check       `yaml:"-"`
	Profiles CheckProfiles `yaml:"-"`
}

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
		ID       string    `yaml:"id"`
		Path     string    `yaml:"path"`
		Provider string    `yaml:"provider"`
		Project  string    `yaml:"project"`
		Scope    string    `yaml:"scope"`
		Checks   yaml.Node `yaml:"checks"`
	}
	var raw repositoryFields
	if err := value.Decode(&raw); err != nil {
		return err
	}
	r.ID, r.Path, r.Provider, r.Project, r.Scope = raw.ID, raw.Path, raw.Provider, raw.Project, raw.Scope
	if raw.Checks.Kind == 0 {
		return nil
	}
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
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	var cfg Config
	decoder := yaml.NewDecoder(f)
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
	if err := cfg.Validate(filepath.Dir(path)); err != nil {
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
			"id": repo.ID, "path": normalizedPath, "project": repo.Project, "scope": repo.Scope,
		} {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("repository %d %s is required", i, field)
			}
			if _, exists := seen[field][value]; exists {
				return fmt.Errorf("duplicate repository %s %q", field, value)
			}
			seen[field][value] = struct{}{}
		}
		if repo.Provider != "gitlab" && repo.Provider != "github" {
			return fmt.Errorf("provider must be gitlab or github")
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
