package operations

import (
	"context"
	"sort"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/hooks"
)

// Check is one human-readable, JSON-marshallable Doctor finding.
type Check struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// Result is the deterministic, JSON-marshallable Doctor report.
type Result struct {
	Checks []Check `json:"checks"`
}

// ExecutableLookup reports whether an executable can be found.
type ExecutableLookup func(string) (string, error)

// EnvironmentPresent reports only whether an environment variable is present.
type EnvironmentPresent func(string) bool

// RepositoryState inspects one configured repository without changing it.
type RepositoryState func(context.Context, string) (git.State, error)

// Doctor checks local prerequisites and configured repository state.
type Doctor struct {
	config      config.Config
	lookup      ExecutableLookup
	environment EnvironmentPresent
	repository  RepositoryState
	hook        HookInspector
}

// NewDoctor constructs a read-only Doctor service with injected observations.
func NewDoctor(cfg config.Config, lookup ExecutableLookup, environment EnvironmentPresent, repository RepositoryState) *Doctor {
	return NewDoctorWithHookInspector(cfg, lookup, environment, repository, hooks.InspectCommitMsg)
}

// NewDoctorWithHookInspector constructs a read-only Doctor service with an injected hook observation.
func NewDoctorWithHookInspector(cfg config.Config, lookup ExecutableLookup, environment EnvironmentPresent, repository RepositoryState, inspector HookInspector) *Doctor {
	if inspector == nil {
		inspector = hooks.InspectCommitMsg
	}
	return &Doctor{config: cfg, lookup: lookup, environment: environment, repository: repository, hook: inspector}
}

// Run returns all checks in a stable order and does not mutate the workspace.
func (d *Doctor) Run(ctx context.Context) (Result, error) {
	result := Result{Checks: make([]Check, 0)}
	for _, executable := range []string{"git", "smt", "lefthook"} {
		result.Checks = append(result.Checks, d.executableCheck(executable))
	}

	for _, repository := range d.config.Repositories {
		result.Checks = append(result.Checks, d.repositoryCheck(ctx, repository))
	}
	for _, repository := range d.config.Repositories {
		result.Checks = append(result.Checks, d.hookCheck(repository))
	}

	for _, executable := range d.profileExecutables() {
		result.Checks = append(result.Checks, d.executableCheck(executable))
	}

	providers := map[string]string{}
	for _, repository := range d.config.Repositories {
		switch repository.Provider {
		case "gitlab":
			providers["gitlab"] = "SMT_GITLAB_TOKEN"
		case "github":
			providers["github"] = "SMT_GITHUB_TOKEN"
		}
	}
	for _, provider := range []string{"gitlab", "github"} {
		if variable, ok := providers[provider]; ok {
			result.Checks = append(result.Checks, d.tokenCheck(provider, variable))
		}
	}

	return result, nil
}

func (d *Doctor) hookCheck(repository config.Repository) Check {
	check := Check{ID: "hook:" + repository.ID + ":commit-msg"}
	if d.hook == nil {
		check.Status = "error"
		check.Message = "repository " + repository.ID + " commit-msg hook could not be inspected"
		return check
	}
	status, err := d.hook(repository.Path)
	if err != nil {
		check.Status = "error"
		check.Message = "repository " + repository.ID + " commit-msg hook could not be inspected"
		return check
	}
	check.Status = "ok"
	if status == hooks.HookAbsent || status == hooks.HookUnmanaged {
		check.Status = "warning"
	}
	check.Message = "repository " + repository.ID + " commit-msg hook is " + string(status)
	return check
}

func (d *Doctor) executableCheck(name string) Check {
	if d.lookup == nil {
		return Check{ID: "tool:" + name, Status: "error", Message: name + " executable is not available"}
	}
	if _, err := d.lookup(name); err != nil {
		return Check{ID: executableID(name), Status: "error", Message: name + " executable is not available"}
	}
	return Check{ID: executableID(name), Status: "ok", Message: name + " executable is available"}
}

func executableID(name string) string {
	if name == "git" {
		return "git"
	}
	return "tool:" + name
}

func (d *Doctor) repositoryCheck(ctx context.Context, repository config.Repository) Check {
	check := Check{ID: "repo:" + repository.ID + ":worktree"}
	if d.repository == nil {
		check.Status = "error"
		check.Message = "repository " + repository.ID + " is not an initialized Git worktree"
		return check
	}
	state, err := d.repository(ctx, repository.Path)
	if err != nil || !state.Initialized {
		check.Status = "error"
		check.Message = "repository " + repository.ID + " is not an initialized Git worktree"
		return check
	}
	check.Status = "ok"
	check.Message = "repository " + repository.ID + " is an initialized Git worktree"
	return check
}

func (d *Doctor) tokenCheck(provider, variable string) Check {
	set := d.environment != nil && d.environment(variable)
	status := "error"
	message := variable + " is not set"
	if set {
		status = "ok"
		message = variable + " is set"
	}
	return Check{ID: "token:" + provider, Status: status, Message: message}
}

func (d *Doctor) profileExecutables() []string {
	core := map[string]struct{}{"git": {}, "smt": {}, "lefthook": {}}
	seen := make(map[string]struct{})
	for _, repository := range d.config.Repositories {
		for _, checks := range repository.Profiles {
			for _, check := range checks {
				if len(check.Argv) > 0 && check.Argv[0] != "" {
					if _, ok := core[check.Argv[0]]; ok {
						continue
					}
					seen[check.Argv[0]] = struct{}{}
				}
			}
		}
	}
	result := make([]string, 0, len(seen))
	for executable := range seen {
		result = append(result, executable)
	}
	sort.Strings(result)
	return result
}
