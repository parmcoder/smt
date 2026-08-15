package workspace

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
)

type lifecycleRunner struct {
	calls    []string
	fail     string
	branches map[string]bool
	events   *[]string
}

func (r *lifecycleRunner) Run(_ context.Context, dir string, args ...string) (git.Result, error) {
	call := dir + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if r.events != nil {
		*r.events = append(*r.events, "git:"+call)
	}
	if r.events != nil {
		*r.events = append(*r.events, "git:"+call)
	}
	if r.fail != "" && strings.Contains(call, r.fail) {
		return git.Result{ExitCode: 1}, errors.New("secret failure")
	}
	switch args[0] {
	case "rev-parse":
		return git.Result{Stdout: "true\n", ExitCode: 0}, nil
	case "status":
		return git.Result{}, nil
	case "symbolic-ref":
		return git.Result{Stdout: "main\n"}, nil
	case "show-ref":
		ref := args[len(args)-1]
		if r.branches[dir+"="+ref] {
			return git.Result{}, nil
		}
		return git.Result{ExitCode: 1}, errors.New("missing")
	default:
		return git.Result{}, nil
	}
}

type lifecycleBeads struct {
	created int
	issue   beads.Issue
	events  *[]string
}

func (b *lifecycleBeads) CreatePreparedWorkspaceTask(context.Context) (string, error) {
	b.created++
	if b.events != nil {
		*b.events = append(*b.events, "beads:create")
	}
	if b.events != nil {
		*b.events = append(*b.events, "beads:create")
	}
	return "task-7", nil
}
func (b *lifecycleBeads) ShowIssue(context.Context, string) (beads.Issue, error) { return b.issue, nil }

func lifecycleConfig() config.Config {
	return config.Config{Repositories: []config.Repository{{ID: "repo", Path: ".", Scope: "repo"}, {ID: "api", Path: "api", Scope: "api"}}, Commit: config.CommitConfig{Types: []string{"feat"}, Scopes: []string{"repo", "api"}}}
}

func TestPreparePreflightsEveryRepositoryBeforeCreateAndUsesGeneratedID(t *testing.T) {
	events := []string{}
	r := &lifecycleRunner{branches: map[string]bool{"/root=refs/heads/main": true, "/root/api=refs/heads/main": true}, events: &events}
	b := &lifecycleBeads{events: &events}
	report, err := Prepare(context.Background(), lifecycleConfig(), "/root", r, b)
	if err != nil || b.created != 1 || report.TaskID != "task-7" {
		t.Fatalf("report=%+v created=%d err=%v calls=%v", report, b.created, err, r.calls)
	}
	create := eventIndex(events, "beads:create")
	apiDefault := eventContainsIndex(events, "/root/api symbolic-ref")
	if create < 0 || apiDefault < 0 || create < apiDefault {
		t.Fatalf("events=%v", events)
	}
	for _, call := range r.calls {
		if strings.Contains(call, "switch --create") && !strings.Contains(call, "task-7") {
			t.Fatalf("non-exact branch call=%s", call)
		}
	}
}

func TestPrepareBranchPreflightFailureDoesNotStash(t *testing.T) {
	r := &lifecycleRunner{branches: map[string]bool{"/root=refs/heads/main": true, "/root/api=refs/heads/main": true}}
	b := &lifecycleBeads{}
	// The generated target exists in one repository; the implementation must stop before stash.
	r.branches["/root/api=refs/heads/task-7"] = true
	_, err := Prepare(context.Background(), lifecycleConfig(), "/root", r, b)
	if err == nil {
		t.Fatal("expected branch preflight failure")
	}
	for _, call := range r.calls {
		if strings.Contains(call, "stash") {
			t.Fatalf("stash before preflight: %v", r.calls)
		}
	}
}

func TestPrepareMissingDefaultBranchDoesNotCreateTaskOrStash(t *testing.T) {
	events := []string{}
	r := &lifecycleRunner{branches: map[string]bool{"/root=refs/heads/main": true}, events: &events}
	b := &lifecycleBeads{events: &events}
	if _, err := Prepare(context.Background(), lifecycleConfig(), "/root", r, b); err == nil {
		t.Fatal("expected missing default branch")
	}
	if b.created != 0 || eventIndex(events, "beads:create") >= 0 || eventContainsIndex(events, "stash") >= 0 {
		t.Fatalf("events=%v created=%d", events, b.created)
	}
}

func TestSwitchRequiresActiveTaskAndAllBranchesBeforeMutation(t *testing.T) {
	r := &lifecycleRunner{branches: map[string]bool{"/root=refs/heads/task-7": true}}
	b := &lifecycleBeads{issue: beads.Issue{ID: "task-7", Status: "closed"}}
	if _, err := Switch(context.Background(), lifecycleConfig(), "/root", "task-7", r, b); err == nil {
		t.Fatal("closed task accepted")
	}
	for _, call := range r.calls {
		if strings.Contains(call, "stash") || strings.Contains(call, "switch ") {
			t.Fatalf("mutation before active check: %v", r.calls)
		}
	}
}

func TestSwitchMissingBranchHasNoGitMutation(t *testing.T) {
	r := &lifecycleRunner{branches: map[string]bool{"/root=refs/heads/task-7": true}}
	b := &lifecycleBeads{issue: beads.Issue{ID: "task-7", Status: "open"}}
	if _, err := Switch(context.Background(), lifecycleConfig(), "/root", "task-7", r, b); err == nil {
		t.Fatal("expected missing branch failure")
	}
	for _, call := range r.calls {
		if strings.Contains(call, "stash") || strings.Contains(call, "fetch") || strings.Contains(call, "switch") {
			t.Fatalf("mutation before branch preflight: %v", r.calls)
		}
	}
}

func TestPrepareStashesTrackedAndUntrackedButLeavesIgnoredFilesAlone(t *testing.T) {
	r := &lifecycleRunner{branches: map[string]bool{"/root=refs/heads/main": true, "/root/api=refs/heads/main": true}}
	_, err := Prepare(context.Background(), lifecycleConfig(), "/root", r, &lifecycleBeads{})
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range r.calls {
		if strings.Contains(call, "stash") {
			if !strings.Contains(call, "--include-untracked") || strings.Contains(call, "--all") {
				t.Fatalf("stash must include tracked/untracked but exclude ignored files: %s", call)
			}
		}
	}
}

func TestPreparePartialFailureRetainsProgressAndNeverRollsBack(t *testing.T) {
	r := &lifecycleRunner{fail: "/root/api switch --create", branches: map[string]bool{"/root=refs/heads/main": true, "/root/api=refs/heads/main": true}}
	report, err := Prepare(context.Background(), lifecycleConfig(), "/root", r, &lifecycleBeads{})
	if err == nil {
		t.Fatal("expected injected second-repository failure")
	}
	if len(report.Results) < 3 || report.Results[2].ID != "repo" || report.Results[2].Status != "created" {
		t.Fatalf("completed progress=%+v", report.Results)
	}
	if len(report.Pending) != 1 || report.Pending[0] != "api" {
		t.Fatalf("pending=%v", report.Pending)
	}
	if !hasCall(r.calls, "/root stash push") || !hasCall(r.calls, "/root switch --create task-7") {
		t.Fatalf("root progress calls=%v", r.calls)
	}
	for _, call := range r.calls {
		if strings.Contains(call, "reset") || strings.Contains(call, "branch -D") || strings.Contains(call, "stash drop") {
			t.Fatalf("unexpected rollback call=%s", call)
		}
	}
}

func eventIndex(events []string, want string) int {
	for i, event := range events {
		if event == want {
			return i
		}
	}
	return -1
}
func eventContainsIndex(events []string, want string) int {
	for i, event := range events {
		if strings.Contains(event, want) {
			return i
		}
	}
	return -1
}
func hasCall(calls []string, want string) bool {
	for _, call := range calls {
		if strings.Contains(call, want) {
			return true
		}
	}
	return false
}
