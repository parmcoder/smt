package operations

import (
	"context"
	"fmt"
	"sort"
	"strings"

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

const (
	DoctorStatusOK      = "ok"
	DoctorStatusWarning = "warning"
	DoctorStatusError   = "error"
)

// DoctorNode is one safe node in the repository-first Doctor report tree.
// Messages are canonical summaries and never contain raw command or private
// inspection output.
type DoctorNode struct {
	ID       string       `json:"id"`
	Status   string       `json:"status"`
	Message  string       `json:"message,omitempty"`
	Children []DoctorNode `json:"children,omitempty"`
}

// DoctorReport is the presentation model for a collected Doctor result.
// Repositories follow configuration order; tools and credentials retain their
// first collection order and are deduplicated by check ID.
type DoctorReport struct {
	Status       string       `json:"status"`
	Repositories []DoctorNode `json:"repositories"`
	Tools        []DoctorNode `json:"tools"`
	Credentials  []DoctorNode `json:"credentials"`
	Unmapped     []DoctorNode `json:"unmapped"`
}

// BuildDoctorReport maps collected checks into a deterministic, safe tree.
// It does not inspect or mutate the workspace and does not expose raw check
// messages in the returned model.
func BuildDoctorReport(cfg config.Config, result Result) DoctorReport {
	report := DoctorReport{
		Status:       DoctorStatusOK,
		Repositories: make([]DoctorNode, 0, len(cfg.Repositories)),
		Tools:        make([]DoctorNode, 0),
		Credentials:  make([]DoctorNode, 0),
		Unmapped:     make([]DoctorNode, 0),
	}

	knownRepositoryChecks := make(map[string]struct{}, len(cfg.Repositories)*2)
	checksByID := make(map[string][]Check, len(result.Checks))
	for _, repository := range cfg.Repositories {
		knownRepositoryChecks["repo:"+repository.ID+":worktree"] = struct{}{}
		knownRepositoryChecks["hook:"+repository.ID+":commit-msg"] = struct{}{}
		knownRepositoryChecks["repo:"+repository.ID+":default"] = struct{}{}
		knownRepositoryChecks["repo:"+repository.ID+":state"] = struct{}{}
	}
	for _, check := range result.Checks {
		checksByID[check.ID] = append(checksByID[check.ID], check)
		switch {
		case isToolCheck(check.ID):
			report.Tools = appendDoctorNodeOnce(report.Tools, check)
		case isCredentialCheck(check.ID):
			report.Credentials = appendDoctorNodeOnce(report.Credentials, check)
		case hasKnownCheck(knownRepositoryChecks, check.ID):
		default:
			report.Unmapped = append(report.Unmapped, doctorNode(check))
		}
	}

	for _, repository := range cfg.Repositories {
		node := DoctorNode{
			ID:       "repo:" + repository.ID,
			Status:   DoctorStatusOK,
			Message:  "repository " + repository.ID,
			Children: make([]DoctorNode, 0, 4),
		}
		for _, checkID := range []string{
			"repo:" + repository.ID + ":worktree",
			"repo:" + repository.ID + ":default",
			"repo:" + repository.ID + ":state",
			"hook:" + repository.ID + ":commit-msg",
		} {
			for _, check := range checksByID[checkID] {
				child := doctorNode(check)
				node.Children = append(node.Children, child)
				node.Status = worseDoctorStatus(node.Status, child.Status)
			}
		}

		remote := DoctorNode{ID: "remote:" + repository.ID, Status: DoctorStatusOK}
		if strings.TrimSpace(repository.Remote.URL) == "" {
			remote.Status = DoctorStatusWarning
			remote.Message = "repository " + repository.ID + " remote is not configured"
		} else {
			remote.Message = "repository " + repository.ID + " remote is configured"
		}
		node.Children = append(node.Children, remote)
		node.Status = worseDoctorStatus(node.Status, remote.Status)

		provider := DoctorNode{ID: "provider:" + repository.ID, Status: DoctorStatusOK}
		if repository.Provider == "" {
			provider.Message = "repository " + repository.ID + " uses local-only provider configuration"
		} else {
			provider.Message = fmt.Sprintf("repository %s provider %s project %s is configured", repository.ID, repository.Provider, repository.Project)
		}
		node.Children = append(node.Children, provider)
		node.Status = worseDoctorStatus(node.Status, provider.Status)
		report.Repositories = append(report.Repositories, node)
		report.Status = worseDoctorStatus(report.Status, node.Status)
	}

	for _, node := range report.Tools {
		report.Status = worseDoctorStatus(report.Status, node.Status)
	}
	for _, node := range report.Credentials {
		report.Status = worseDoctorStatus(report.Status, node.Status)
	}
	for _, node := range report.Unmapped {
		report.Status = worseDoctorStatus(report.Status, node.Status)
	}
	return report
}

func hasKnownCheck(checks map[string]struct{}, id string) bool {
	_, ok := checks[id]
	return ok
}

func isToolCheck(id string) bool {
	return id == "git" || id == "beads:workspace" || strings.HasPrefix(id, "tool:")
}

func isCredentialCheck(id string) bool {
	return strings.HasPrefix(id, "token:")
}

func appendDoctorNodeOnce(nodes []DoctorNode, check Check) []DoctorNode {
	for i := range nodes {
		if nodes[i].ID != check.ID {
			continue
		}
		candidate := doctorNode(check)
		status := worseDoctorStatus(nodes[i].Status, candidate.Status)
		if status != nodes[i].Status {
			nodes[i].Status = status
			nodes[i].Message = candidate.Message
		}
		return nodes
	}
	return append(nodes, doctorNode(check))
}

func doctorNode(check Check) DoctorNode {
	return DoctorNode{ID: check.ID, Status: check.Status, Message: safeDoctorMessage(check)}
}

func safeDoctorMessage(check Check) string {
	switch {
	case check.ID == "git" || strings.HasPrefix(check.ID, "tool:"):
		name := strings.TrimPrefix(check.ID, "tool:")
		return fmt.Sprintf("%s executable is %s", name, availability(check.Status))
	case strings.HasPrefix(check.ID, "repo:") && strings.HasSuffix(check.ID, ":worktree"):
		repositoryID := strings.TrimSuffix(strings.TrimPrefix(check.ID, "repo:"), ":worktree")
		if check.Status == DoctorStatusOK {
			return "repository " + repositoryID + " is an initialized Git worktree"
		}
		return "repository " + repositoryID + " is not an initialized Git worktree"
	case strings.HasPrefix(check.ID, "repo:") && strings.HasSuffix(check.ID, ":default"):
		return "repository " + strings.TrimSuffix(strings.TrimPrefix(check.ID, "repo:"), ":default") + " default branch is configured"
	case strings.HasPrefix(check.ID, "repo:") && strings.HasSuffix(check.ID, ":state"):
		if check.Status == DoctorStatusError {
			return "repository " + strings.TrimSuffix(strings.TrimPrefix(check.ID, "repo:"), ":state") + " is detached"
		}
		if check.Status == DoctorStatusWarning {
			return "repository " + strings.TrimSuffix(strings.TrimPrefix(check.ID, "repo:"), ":state") + " is not clean"
		}
		return "repository " + strings.TrimSuffix(strings.TrimPrefix(check.ID, "repo:"), ":state") + " is clean and attached"
	case strings.HasPrefix(check.ID, "hook:") && strings.HasSuffix(check.ID, ":commit-msg"):
		repositoryID := strings.TrimSuffix(strings.TrimPrefix(check.ID, "hook:"), ":commit-msg")
		if check.Status == DoctorStatusError {
			return "repository " + repositoryID + " commit-msg hook could not be inspected"
		}
		state := "current"
		if check.Status == DoctorStatusWarning {
			state = "absent"
			if strings.Contains(strings.ToLower(check.Message), "unmanaged") {
				state = "unmanaged"
			}
		}
		return "repository " + repositoryID + " commit-msg hook is " + state
	case strings.HasPrefix(check.ID, "token:"):
		provider := strings.TrimPrefix(check.ID, "token:")
		variable := "SMT_" + strings.ToUpper(provider) + "_TOKEN"
		if check.Status == DoctorStatusOK {
			return variable + " is set"
		}
		return variable + " is not set"
	case check.ID == "beads:workspace":
		if check.Status == DoctorStatusOK {
			return "Beads workspace is ready"
		}
		return "Beads workspace is unavailable"
	default:
		return ""
	}
}

func availability(status string) string {
	if status == DoctorStatusOK {
		return "available"
	}
	return "not available"
}

func worseDoctorStatus(current, candidate string) string {
	if doctorStatusRank(candidate) > doctorStatusRank(current) {
		return candidate
	}
	return current
}

func doctorStatusRank(status string) int {
	switch status {
	case DoctorStatusError:
		return 3
	case DoctorStatusWarning:
		return 2
	case DoctorStatusOK:
		return 1
	default:
		return 0
	}
}

// ExecutableLookup reports whether an executable can be found.
type ExecutableLookup func(string) (string, error)

// EnvironmentPresent reports only whether an environment variable is present.
type EnvironmentPresent func(string) bool

// RepositoryState inspects one configured repository without changing it.
type RepositoryState func(context.Context, string) (git.State, error)

// BeadsAvailability checks the configured workspace without exposing command output.
type BeadsAvailability func(context.Context) error

// ActiveBranchLookup verifies that a shared non-default branch is an active Beads ID.
type ActiveBranchLookup func(context.Context, string) (bool, error)

// Doctor checks local prerequisites and configured repository state.
type Doctor struct {
	config      config.Config
	lookup      ExecutableLookup
	environment EnvironmentPresent
	repository  RepositoryState
	hook        HookInspector
	beads       BeadsAvailability
	active      ActiveBranchLookup
}

// NewDoctorWithBeads adds read-only Beads workspace readiness to Doctor.
func NewDoctorWithBeads(cfg config.Config, lookup ExecutableLookup, environment EnvironmentPresent, repository RepositoryState, beads BeadsAvailability) *Doctor {
	doctor := NewDoctorWithHookInspector(cfg, lookup, environment, repository, hooks.InspectCommitMsg)
	doctor.beads = beads
	return doctor
}

// SetActiveBranchLookup supplies active Beads-ID validation for branch alignment.
func (d *Doctor) SetActiveBranchLookup(lookup ActiveBranchLookup) { d.active = lookup }

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

	states := make(map[string]git.State, len(d.config.Repositories))
	for _, repository := range d.config.Repositories {
		check, state := d.repositoryCheck(ctx, repository)
		result.Checks = append(result.Checks, check)
		if state.Initialized {
			states[repository.ID] = state
		}
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
	for _, repository := range d.config.Repositories {
		state, ok := states[repository.ID]
		if !ok {
			continue
		}
		result.Checks = append(result.Checks,
			Check{ID: "repo:" + repository.ID + ":default", Status: DoctorStatusOK, Message: "repository " + repository.ID + " default branch is " + repository.EffectiveDefaultBranch()},
			repositoryStateCheck(repository, state),
		)
	}
	result.Checks = append(result.Checks, d.branchAlignmentCheck(ctx, states))
	beadsCheck := Check{ID: "beads:workspace", Status: DoctorStatusOK, Message: "Beads workspace is ready"}
	if d.beads != nil {
		if err := d.beads(ctx); err != nil {
			beadsCheck.Status = DoctorStatusWarning
			beadsCheck.Message = "Beads workspace is unavailable"
		}
	}
	result.Checks = append(result.Checks, beadsCheck)

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

func (d *Doctor) repositoryCheck(ctx context.Context, repository config.Repository) (Check, git.State) {
	check := Check{ID: "repo:" + repository.ID + ":worktree"}
	if d.repository == nil {
		check.Status = "error"
		check.Message = "repository " + repository.ID + " is not an initialized Git worktree"
		return check, git.State{}
	}
	state, err := d.repository(ctx, repository.Path)
	if err != nil || !state.Initialized {
		check.Status = "error"
		check.Message = "repository " + repository.ID + " is not an initialized Git worktree"
		return check, state
	}
	check.Status = "ok"
	check.Message = "repository " + repository.ID + " is an initialized Git worktree"
	return check, state
}

func repositoryStateCheck(repository config.Repository, state git.State) Check {
	check := Check{ID: "repo:" + repository.ID + ":state", Status: DoctorStatusOK, Message: "repository " + repository.ID + " is clean and attached"}
	if state.Detached {
		check.Status = DoctorStatusError
		check.Message = "repository " + repository.ID + " is detached; switch it to a branch"
	} else if state.Dirty {
		check.Status = DoctorStatusWarning
		check.Message = "repository " + repository.ID + " is not clean; commit or stash local changes"
	}
	return check
}

func (d *Doctor) branchAlignmentCheck(ctx context.Context, states map[string]git.State) Check {
	check := Check{ID: "workspace:branches", Status: DoctorStatusOK, Message: "repositories are aligned on their effective default branches"}
	if len(states) != len(d.config.Repositories) || len(states) == 0 {
		check.Status = DoctorStatusError
		check.Message = "repositories must be initialized and attached before branch alignment can be checked"
		return check
	}
	allDefault := true
	shared := ""
	for _, repository := range d.config.Repositories {
		state := states[repository.ID]
		if state.Branch != repository.EffectiveDefaultBranch() {
			allDefault = false
		}
		if shared == "" {
			shared = state.Branch
		} else if shared != state.Branch {
			check.Status = DoctorStatusError
			check.Message = "repositories must share one effective default branch or one active Beads-ID branch"
			return check
		}
	}
	if allDefault {
		return check
	}
	if d.active != nil {
		active, err := d.active(ctx, shared)
		if err != nil || !active {
			check.Status = DoctorStatusError
			check.Message = "repositories use a non-default branch that is not an active Beads-ID branch"
		}
		return check
	}
	check.Status = DoctorStatusError
	check.Message = "repositories use a shared non-default branch but active Beads-ID status could not be verified"
	return check
}

func (d *Doctor) tokenCheck(provider, variable string) Check {
	set := d.environment != nil && d.environment(variable)
	status := "warning"
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
