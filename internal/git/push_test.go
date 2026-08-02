package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	ggit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func TestPlanPushPreflightsAllBeforeAnyRemoteMutation(t *testing.T) {
	root := newCommittedRepository(t, "root")
	dirty := newCommittedRepository(t, "dirty")
	if err := os.WriteFile(filepath.Join(dirty, "dirty"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := PlanPush(context.Background(), []PushTarget{{Repository: Repository{ID: "child", Dir: root}, RemoteURL: "file:///unused-child"}, {Repository: Repository{ID: "root", Dir: dirty, IsRoot: true}, RemoteURL: "file:///unused-root"}})
	if err == nil || !contains(err.Error(), "repository is dirty") {
		t.Fatalf("error=%v", err)
	}
}

func TestPlanPushRejectsDetachedBeforeExecution(t *testing.T) {
	dir := newCommittedRepository(t, "detached")
	repository, err := ggit.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := repository.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, head.Hash())); err != nil {
		t.Fatal(err)
	}
	_, err = PlanPush(context.Background(), []PushTarget{{Repository: Repository{ID: "repo", Dir: dir, IsRoot: true}, RemoteURL: "file:///unused"}})
	if err == nil || !contains(err.Error(), "detached") {
		t.Fatalf("error=%v", err)
	}
}

func TestExecutePushOrdersChildrenReportsPartialFailureAndDryRunIsImmutable(t *testing.T) {
	root := newCommittedRepository(t, "root")
	web := newCommittedRepository(t, "web")
	api := newCommittedRepository(t, "api")
	plan, err := PlanPush(context.Background(), []PushTarget{{Repository: Repository{ID: "repo", Dir: root, IsRoot: true}, RemoteURL: "file:///root"}, {Repository: Repository{ID: "web", Dir: web}, RemoteURL: "file:///web"}, {Repository: Repository{ID: "api", Dir: api}, RemoteURL: "file:///api"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(plan.Steps); !reflect.DeepEqual(got, []string{"web", "api", "repo"}) {
		t.Fatalf("plan order=%v", got)
	}
	original := pushStep
	defer func() { pushStep = original }()
	var calls []string
	pushStep = func(_ context.Context, step PushStep) error {
		calls = append(calls, step.Repository.ID)
		if step.Repository.ID == "api" {
			return errors.New("remote rejected")
		}
		return nil
	}
	report, err := ExecutePush(context.Background(), plan, false)
	if err == nil {
		t.Fatal("expected failure")
	}
	if !reflect.DeepEqual(calls, []string{"web", "api"}) {
		t.Fatalf("calls=%v", calls)
	}
	if got := ids(report.Pushed); !reflect.DeepEqual(got, []string{"web"}) {
		t.Fatalf("pushed=%v", got)
	}
	if got := ids(report.Pending); !reflect.DeepEqual(got, []string{"repo"}) {
		t.Fatalf("pending=%v", got)
	}
	calls = nil
	report, err = ExecutePush(context.Background(), plan, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 0 || !report.DryRun || len(report.Pushed) != 0 {
		t.Fatalf("dry report=%#v calls=%v", report, calls)
	}
}

func TestExecutePushUsesConfiguredFileRemotesInChildFirstOrder(t *testing.T) {
	root := newCommittedRepository(t, "root")
	web := newCommittedRepository(t, "web")
	api := newCommittedRepository(t, "api")
	remoteRoot := t.TempDir()
	targets := []PushTarget{
		{Repository: Repository{ID: "repo", Dir: root, IsRoot: true}, RemoteURL: newBareRemote(t, remoteRoot, "repo")},
		{Repository: Repository{ID: "web", Dir: web}, RemoteURL: newBareRemote(t, remoteRoot, "web")},
		{Repository: Repository{ID: "api", Dir: api}, RemoteURL: newBareRemote(t, remoteRoot, "api")},
	}
	plan, err := PlanPush(context.Background(), targets)
	if err != nil {
		t.Fatal(err)
	}
	report, err := ExecutePush(context.Background(), plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(report.Pushed); !reflect.DeepEqual(got, []string{"web", "api", "repo"}) {
		t.Fatalf("pushed=%v", got)
	}
	for _, target := range targets {
		remote, err := ggit.PlainOpen(target.RemoteURL)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := remote.Reference(plumbing.NewBranchReferenceName("master"), true); err != nil {
			t.Fatalf("remote %s missing pushed branch: %v", target.Repository.ID, err)
		}
	}
}

func newCommittedRepository(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	repository, err := ggit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte(name), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("file"); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("initial", signature()); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newBareRemote(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name+".git")
	if _, err := ggit.PlainInit(dir, true); err != nil {
		t.Fatal(err)
	}
	return dir
}
func ids(steps []PushStep) []string {
	result := make([]string, len(steps))
	for i, step := range steps {
		result[i] = step.Repository.ID
	}
	return result
}
func contains(value, part string) bool { return strings.Contains(value, part) }
