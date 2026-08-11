// Package remote implements provider-backed project provisioning and local
// remote wiring without storing provider credentials.
package remote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/provider"
	"gopkg.in/yaml.v3"
)

type ProjectStatus string

const (
	StatusExisting ProjectStatus = "existing"
	StatusCreated  ProjectStatus = "created"
	StatusPending  ProjectStatus = "pending"
	StatusWired    ProjectStatus = "configured"
)

// ProjectResult is safe to serialize: it contains no token, response body, or
// authorization header.
type ProjectResult struct {
	ID      string        `json:"id"`
	Project string        `json:"project"`
	Status  ProjectStatus `json:"status"`
	SSHURL  string        `json:"ssh_url,omitempty"`
	WebURL  string        `json:"web_url,omitempty"`
	Error   string        `json:"error,omitempty"`
}

// Report describes provider progress and local wiring progress.
type Report struct {
	DryRun      bool            `json:"dry_run"`
	Projects    []ProjectResult `json:"projects"`
	Configured  []string        `json:"configured,omitempty"`
	Pending     []string        `json:"pending,omitempty"`
	WiringError string          `json:"wiring_error,omitempty"`
}

// Factory constructs one project provider for a configured provider name.
type Factory func(string, config.ProviderConfig, string) (provider.ProjectProvider, error)

// TokenLookup returns an environment-only provider token.
type TokenLookup func(string) string

// Provision performs read-only preflight, provider discovery/creation, and
// finally local wiring. It never deletes or rolls back a remote project.
func Provision(ctx context.Context, cfg config.Config, workspaceRoot string, runner git.Runner, factory Factory, token TokenLookup, dryRun bool) (Report, error) {
	report := Report{DryRun: dryRun, Projects: make([]ProjectResult, 0, len(cfg.Repositories))}
	if runner == nil || factory == nil || token == nil {
		return report, errors.New("remote provision: required services are unavailable")
	}
	root, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return report, errors.New("remote provision: workspace path is invalid")
	}
	targets := append([]config.Repository(nil), cfg.Repositories...)
	sort.SliceStable(targets, func(i, j int) bool {
		return filepath.Clean(targets[i].Path) != "." && filepath.Clean(targets[j].Path) == "."
	})
	providers := make(map[string]provider.ProjectProvider)
	for _, repository := range targets {
		if repository.Provider == "" || repository.Project == "" {
			return report, fmt.Errorf("remote provision: repository %s requires provider and project", repository.ID)
		}
		if strings.TrimSpace(token(repository.Provider)) == "" {
			return report, fmt.Errorf("remote provision: %s provider token is not configured", repository.Provider)
		}
		if _, ok := providers[repository.Provider]; ok {
			continue
		}
		settings := cfg.Providers.GitHub
		if repository.Provider == "gitlab" {
			settings = cfg.Providers.GitLab
		}
		projectProvider, factoryErr := factory(repository.Provider, settings, token(repository.Provider))
		if factoryErr != nil {
			return report, fmt.Errorf("remote provision: %w", factoryErr)
		}
		providers[repository.Provider] = projectProvider
	}

	localOrigins := make(map[string]string, len(targets))
	for _, repository := range targets {
		directory := filepath.Join(root, repository.Path)
		state, inspectErr := git.Inspect(ctx, runner, git.Repository{ID: repository.ID, Dir: directory, IsRoot: filepath.Clean(repository.Path) == "."})
		if inspectErr != nil {
			return report, fmt.Errorf("remote provision: %w", inspectErr)
		}
		if !state.Initialized {
			return report, fmt.Errorf("remote provision: repository %s is not initialized", repository.ID)
		}
		if state.Dirty {
			return report, fmt.Errorf("remote provision: repository %s is dirty", repository.ID)
		}
		if state.Detached || state.Branch == "" {
			return report, fmt.Errorf("remote provision: repository %s is detached", repository.ID)
		}
		origin, originErr := readOrigin(ctx, runner, directory)
		if originErr != nil {
			return report, fmt.Errorf("remote provision: %w", originErr)
		}
		localOrigins[repository.ID] = origin
	}

	available := make(map[string]provider.ProjectInfo, len(targets))
	missing := make([]config.Repository, 0, len(targets))
	for index, repository := range targets {
		projectInfo, inspectErr := providers[repository.Provider].InspectProject(ctx, repository.Project)
		if inspectErr != nil {
			report.Projects = append(report.Projects, ProjectResult{ID: repository.ID, Project: repository.Project, Status: StatusPending, Error: safeError(inspectErr)})
			report.Pending = appendPending(report.Pending, targets[index:])
			return report, fmt.Errorf("remote provision: inspect repository %s: %w", repository.ID, inspectErr)
		}
		if projectInfo.Exists {
			if projectInfo.Project != repository.Project || projectInfo.SSHURL == "" || projectInfo.WebURL == "" {
				report.Projects = append(report.Projects, ProjectResult{ID: repository.ID, Project: repository.Project, Status: StatusPending, Error: "existing provider project is not compatible"})
				report.Pending = appendPending(report.Pending, targets[index:])
				return report, fmt.Errorf("remote provision: existing project for repository %s is not compatible", repository.ID)
			}
			available[repository.ID] = projectInfo
			report.Projects = append(report.Projects, projectResult(repository, StatusExisting, projectInfo))
			continue
		}
		missing = append(missing, repository)
		report.Projects = append(report.Projects, ProjectResult{ID: repository.ID, Project: repository.Project, Status: StatusPending})
		report.Pending = append(report.Pending, repository.ID)
	}
	for _, repository := range targets {
		projectInfo, exists := available[repository.ID]
		origin := localOrigins[repository.ID]
		if !exists {
			if origin != "" || repository.Remote.URL != "" {
				return report, fmt.Errorf("remote provision: repository %s has a local remote conflict", repository.ID)
			}
			continue
		}
		if origin != "" && origin != projectInfo.SSHURL {
			return report, fmt.Errorf("remote provision: repository %s has a conflicting origin", repository.ID)
		}
		if repository.Remote.URL != "" && repository.Remote.URL != projectInfo.SSHURL {
			return report, fmt.Errorf("remote provision: repository %s has a conflicting configured remote", repository.ID)
		}
	}
	if dryRun {
		return report, nil
	}
	// Missing projects are pending in a dry-run plan, but once creation starts
	// the pending list must contain only projects not yet available.
	report.Pending = nil
	for index, repository := range missing {
		created, createErr := providers[repository.Provider].CreateProject(ctx, provider.ProjectSpec{Project: repository.Project, Visibility: repository.EffectiveVisibility()})
		if createErr != nil {
			setProjectResult(&report, repository.ID, ProjectResult{ID: repository.ID, Project: repository.Project, Status: StatusPending, Error: safeError(createErr)})
			report.Pending = appendPending(report.Pending, missing[index:])
			return report, fmt.Errorf("remote provision: create repository %s: %w", repository.ID, createErr)
		}
		available[repository.ID] = created
		setProjectResult(&report, repository.ID, projectResult(repository, StatusCreated, created))
	}
	// Every provider target is now available; pending should describe only
	// subsequent local wiring failures.
	report.Pending = nil

	updated := cfg
	for index := range updated.Repositories {
		updated.Repositories[index].Remote.URL = available[updated.Repositories[index].ID].SSHURL
	}
	if err := writeConfig(filepath.Join(root, "smt.yaml"), updated); err != nil {
		report.Pending = appendPending(report.Pending, targets)
		return report, fmt.Errorf("remote provision: %w", err)
	}
	if err := writeGitmodules(filepath.Join(root, ".gitmodules"), updated.Repositories); err != nil {
		report.Pending = appendPending(report.Pending, targets)
		return report, fmt.Errorf("remote provision: %w", err)
	}
	for index, repository := range targets {
		directory := filepath.Join(root, repository.Path)
		if err := setOrigin(ctx, runner, directory, available[repository.ID].SSHURL); err != nil {
			report.WiringError = safeError(err)
			report.Pending = appendPending(report.Pending, targets[index:])
			return report, fmt.Errorf("remote provision: %w", err)
		}
		report.Configured = append(report.Configured, repository.ID)
	}
	return report, nil
}

func setProjectResult(report *Report, id string, replacement ProjectResult) {
	for index := range report.Projects {
		if report.Projects[index].ID == id {
			report.Projects[index] = replacement
			return
		}
	}
	report.Projects = append(report.Projects, replacement)
}

func projectResult(repository config.Repository, status ProjectStatus, info provider.ProjectInfo) ProjectResult {
	return ProjectResult{ID: repository.ID, Project: repository.Project, Status: status, SSHURL: info.SSHURL, WebURL: info.WebURL}
}

func appendPending(existing []string, repositories []config.Repository) []string {
	for _, repository := range repositories {
		if !contains(existing, repository.ID) {
			existing = append(existing, repository.ID)
		}
	}
	return existing
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func safeError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func readOrigin(ctx context.Context, runner git.Runner, directory string) (string, error) {
	result, err := runner.Run(ctx, directory, "remote", "get-url", "origin")
	if result.ExitCode != 0 {
		return "", nil
	}
	if err != nil {
		return "", errors.New("inspect repository origin")
	}
	return strings.TrimSpace(result.Stdout), nil
}

func setOrigin(ctx context.Context, runner git.Runner, directory, remoteURL string) error {
	result, err := runner.Run(ctx, directory, "remote", "get-url", "origin")
	if result.ExitCode == 0 && err == nil {
		result, err = runner.Run(ctx, directory, "remote", "set-url", "origin", remoteURL)
	} else {
		result, err = runner.Run(ctx, directory, "remote", "add", "origin", remoteURL)
	}
	if err != nil || result.ExitCode != 0 {
		return errors.New("configure repository origin")
	}
	return nil
}

func writeConfig(path string, cfg config.Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return errors.New("encode smt.yaml")
	}
	return writeAtomic(path, data)
}

func writeGitmodules(path string, repositories []config.Repository) error {
	children := make([]config.Repository, 0, len(repositories))
	for _, repository := range repositories {
		if filepath.Clean(repository.Path) != "." {
			children = append(children, repository)
		}
	}
	if len(children) == 0 {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return errors.New("read .gitmodules")
	}
	updated := updateGitmodules(data, children)
	return writeAtomic(path, []byte(updated))
}

func updateGitmodules(data []byte, repositories []config.Repository) string {
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		lines = nil
	}
	for _, repository := range repositories {
		sectionStart := -1
		pathLine := -1
		urlLine := -1
		for index, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "[submodule ") {
				sectionStart = index
				pathLine, urlLine = -1, -1
			}
			if sectionStart < 0 {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "path = ") && strings.TrimSpace(strings.TrimPrefix(trimmed, "path = ")) == repository.Path {
				pathLine = index
			}
			if pathLine >= 0 && strings.HasPrefix(trimmed, "url = ") {
				urlLine = index
				break
			}
		}
		if urlLine >= 0 {
			lines[urlLine] = "\turl = " + repository.Remote.URL
			continue
		}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		lines = append(lines, fmt.Sprintf("[submodule \"%s\"]", repository.ID), "\tpath = "+repository.Path, "\turl = "+repository.Remote.URL)
	}
	return strings.Join(lines, "\n") + "\n"
}

func writeAtomic(path string, data []byte) error {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".smt-write-*")
	if err != nil {
		return errors.New("create atomic temporary file")
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	if err := temporary.Chmod(mode); err != nil {
		_ = temporary.Close()
		return errors.New("protect atomic temporary file")
	}
	if _, err := temporary.Write(data); err != nil {
		_ = temporary.Close()
		return errors.New("write atomic temporary file")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("close atomic temporary file")
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return errors.New("publish atomic file")
	}
	return nil
}
