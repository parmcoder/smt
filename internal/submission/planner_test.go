package submission

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	workspacepkg "github.com/parmcoder/smt/internal/workspace"
)

type plannerRunner struct {
	counts map[string]int
	origin string
	log    map[string]string
	paths  map[string]string
}

type countingCheckExecutor struct{ calls int }

func (e *countingCheckExecutor) Run(_ context.Context, _ string, _ []string, _ string) error {
	e.calls++
	return nil
}

func (r plannerRunner) Run(_ context.Context, dir string, args ...string) (git.Result, error) {
	if len(args) > 0 && args[0] == "rev-parse" {
		return git.Result{Stdout: "true\n"}, nil
	}
	if len(args) > 0 && args[0] == "status" {
		return git.Result{}, nil
	}
	if len(args) > 0 && args[0] == "symbolic-ref" {
		return git.Result{Stdout: "feature/one\n"}, nil
	}
	if len(args) >= 3 && args[0] == "remote" && args[1] == "get-url" {
		return git.Result{Stdout: r.origin + "\n"}, nil
	}
	if len(args) > 0 && args[0] == "ls-remote" {
		return git.Result{Stdout: "sha\trefs/heads/main\n"}, nil
	}
	if len(args) >= 3 && args[0] == "rev-list" {
		return git.Result{Stdout: strconv.Itoa(r.counts[dir]) + "\n"}, nil
	}
	if len(args) >= 2 && args[0] == "log" {
		return git.Result{Stdout: r.log[dir]}, nil
	}
	if len(args) >= 2 && args[0] == "diff" {
		if len(args) > 4 {
			return git.Result{Stdout: r.paths[dir] + "\n"}, nil
		}
		return git.Result{Stdout: "file.go\n"}, nil
	}
	return git.Result{}, errors.New("unexpected git command")
}

func TestPlanSelectsChangedChildrenBeforeRootAndRequiresAssignedReferences(t *testing.T) {
	root := t.TempDir()
	api := filepath.Join(root, "api")
	web := filepath.Join(root, "web")
	runner := plannerRunner{
		counts: map[string]int{root: 1, api: 1, web: 0},
		origin: "git@example/project.git",
		log: map[string]string{
			root: "root-sha\x00feat(repo): [feature] integrate api\x00",
			api:  "api-sha\x00feat(api): [smt-api-1] add endpoint\x00",
		},
		paths: map[string]string{root: "api"},
	}
	plan, err := Plan(context.Background(), plannerConfig(root), plannerManifest(root), "feature", root, runner, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{plan.Steps[0].ID, plan.Steps[1].ID}; !reflect.DeepEqual(got, []string{"api", "repo"}) {
		t.Fatalf("steps=%v", got)
	}
	if plan.Steps[0].CommitCount != 1 || plan.Steps[1].CommitCount != 1 {
		t.Fatalf("steps=%+v", plan.Steps)
	}
}

func TestPlanRejectsChangedChildWithoutRootGitlink(t *testing.T) {
	root := t.TempDir()
	runner := plannerRunner{
		counts: map[string]int{root: 0, filepath.Join(root, "api"): 1},
		origin: "git@example/project.git",
		log:    map[string]string{filepath.Join(root, "api"): "api-sha\x00feat(api): [smt-api-1] add endpoint\x00"},
	}
	_, err := Plan(context.Background(), plannerConfig(root), plannerManifest(root), "feature", root, runner, nil, false)
	if err == nil || !strings.Contains(err.Error(), "root integration") {
		t.Fatalf("error=%v", err)
	}
}

func TestPlanRejectsWrongAssignedReferenceAndPathMismatch(t *testing.T) {
	root := t.TempDir()
	runner := plannerRunner{counts: map[string]int{root: 1, filepath.Join(root, "api"): 0}, origin: "git@example/project.git", log: map[string]string{root: "root-sha\x00feat(repo): [smt-wrong] change\x00"}}
	_, err := Plan(context.Background(), plannerConfig(root), plannerManifest(root), "feature", root, runner, nil, false)
	if err == nil || !strings.Contains(err.Error(), "invalid assigned commit") {
		t.Fatalf("reference error=%v", err)
	}
	manifest := plannerManifest(root)
	manifest.WorkspacePath = filepath.Join(root, "other")
	if _, err := Plan(context.Background(), plannerConfig(root), manifest, "feature", root, runner, nil, false); err == nil || !strings.Contains(err.Error(), "path does not match") {
		t.Fatalf("path error=%v", err)
	}
}

func TestPlanDryRunDoesNotExecuteSubmitChecks(t *testing.T) {
	root := t.TempDir()
	cfg := plannerConfig(root)
	cfg.Repositories[0].Profiles = config.CheckProfiles{"submit": {{Kind: "command", Argv: []string{"task", "verify"}}}}
	runner := plannerRunner{
		counts: map[string]int{root: 1, filepath.Join(root, "api"): 0, filepath.Join(root, "web"): 0},
		origin: "git@example/project.git",
		log:    map[string]string{root: "root-sha\x00feat(repo): [feature] change\x00"},
	}
	checks := &countingCheckExecutor{}
	if _, err := Plan(context.Background(), cfg, plannerManifest(root), "feature", root, runner, checks, true); err != nil {
		t.Fatal(err)
	}
	if checks.calls != 0 {
		t.Fatalf("submit checks ran %d times during dry-run", checks.calls)
	}
}

func TestPlanHandlesRootAfterChildrenInConfigurationOrder(t *testing.T) {
	root := t.TempDir()
	cfg := plannerConfig(root)
	cfg.Repositories = []config.Repository{cfg.Repositories[1], cfg.Repositories[0], cfg.Repositories[2]}
	manifest := plannerManifest(root)
	manifest.Repositories = []workspacepkg.ManifestRepository{manifest.Repositories[1], manifest.Repositories[0], manifest.Repositories[2]}
	runner := plannerRunner{
		counts: map[string]int{root: 1, filepath.Join(root, "api"): 1, filepath.Join(root, "web"): 0},
		origin: "git@example/project.git",
		log: map[string]string{
			root:                       "root-sha\x00feat(repo): [feature] integrate api\x00",
			filepath.Join(root, "api"): "api-sha\x00feat(api): [smt-api-1] add endpoint\x00",
		},
		paths: map[string]string{root: "api"},
	}
	plan, err := Plan(context.Background(), cfg, manifest, "feature", root, runner, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{plan.Steps[0].ID, plan.Steps[1].ID}; !reflect.DeepEqual(got, []string{"api", "repo"}) {
		t.Fatalf("steps=%v", got)
	}
}

func plannerConfig(root string) config.Config {
	return config.Config{Commit: config.CommitConfig{Types: []string{"feat"}, Scopes: []string{"repo", "api", "web"}}, Repositories: []config.Repository{
		{ID: "repo", Path: ".", Scope: "repo", Remote: config.Remote{URL: "git@example/project.git"}},
		{ID: "api", Path: "api", Scope: "api", Remote: config.Remote{URL: "git@example/project.git"}},
		{ID: "web", Path: "web", Scope: "web", Remote: config.Remote{URL: "git@example/project.git"}},
	}}
}

func plannerManifest(root string) workspacepkg.RunManifest {
	return workspacepkg.RunManifest{SchemaVersion: 1, Feature: workspacepkg.FeatureContext{ID: "feature", Title: "Feature"}, WorkspacePath: root, Branch: "feature/one", Repositories: []workspacepkg.ManifestRepository{
		{ID: "repo", Path: ".", BaseBranch: "main", BaseCommit: "repo-base"},
		{ID: "api", Path: "api", BaseBranch: "main", BaseCommit: "api-base", Tasks: []workspacepkg.TaskAssignment{{ID: "smt-api-1", AllowedReferences: []string{"smt-api-1"}}}},
		{ID: "web", Path: "web", BaseBranch: "main", BaseCommit: "web-base", Tasks: []workspacepkg.TaskAssignment{{ID: "smt-web-1", AllowedReferences: []string{"smt-web-1"}}}},
	}}
}
