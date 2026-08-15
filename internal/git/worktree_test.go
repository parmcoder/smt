package git

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPlanWorktreePreflightsGitlinksAndCreatesNestedSteps(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "feature-workspace")
	root := WorktreeTarget{Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, Path: "."}
	web := WorktreeTarget{Repository: Repository{ID: "web", Dir: "/workspace/root/web-app"}, Path: "web-app"}
	runner := &recordingRunner{results: []Result{
		{},
		{Stdout: "true\n"}, {}, {Stdout: "main\n"},
		{Stdout: "true\n"}, {}, {Stdout: "main\n"},
		{ExitCode: 1}, {ExitCode: 1},
		{Stdout: "root-sha\n"}, {Stdout: "child-sha\n"},
		{Stdout: "160000 commit child-sha\tweb-app\n"},
	}}

	plan, err := PlanWorktree(context.Background(), runner, []WorktreeTarget{root, web}, destination, "feature/demo")
	if err != nil {
		t.Fatalf("PlanWorktree() error = %v", err)
	}
	want := []WorktreeStep{
		{Repository: root.Repository, Destination: destination, Branch: "feature/demo", StartPoint: "main"},
		{Repository: web.Repository, Destination: filepath.Join(destination, "web-app"), Branch: "feature/demo", StartPoint: "main"},
	}
	if !reflect.DeepEqual(plan.Steps, want) {
		t.Fatalf("steps = %#v, want %#v", plan.Steps, want)
	}
	for _, call := range runner.calls {
		if len(call.args) > 0 && call.args[0] == "worktree" {
			t.Fatalf("PlanWorktree() created worktree: %#v", call)
		}
	}
}

func TestPlanWorktreeUsesPerRepositoryDefaultBranch(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "feature-workspace")
	root := WorktreeTarget{Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, Path: ".", DefaultBranch: "main"}
	child := WorktreeTarget{Repository: Repository{ID: "api", Dir: "/workspace/root/api"}, Path: "api", DefaultBranch: "trunk"}
	runner := &recordingRunner{results: []Result{{}, {Stdout: "true\n"}, {}, {Stdout: "main\n"}, {Stdout: "true\n"}, {}, {Stdout: "trunk\n"}, {ExitCode: 1}, {ExitCode: 1}, {Stdout: "root-main\n"}, {Stdout: "child-trunk\n"}, {Stdout: "160000 commit child-trunk\tapi\n"}}}
	plan, err := PlanWorktree(context.Background(), runner, []WorktreeTarget{root, child}, destination, "feature/demo")
	if err != nil || plan.Steps[0].StartPoint != "main" || plan.Steps[1].StartPoint != "trunk" {
		t.Fatalf("plan=%+v err=%v", plan, err)
	}
}

func TestPlanWorktreeGitlinkValidationUsesRootDefaultRef(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "feature-workspace")
	root := WorktreeTarget{Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, Path: ".", DefaultBranch: "main"}
	child := WorktreeTarget{Repository: Repository{ID: "api", Dir: "/workspace/root/api"}, Path: "api", DefaultBranch: "trunk"}
	runner := &recordingRunner{results: []Result{{}, {Stdout: "true\n"}, {}, {Stdout: "main\n"}, {Stdout: "true\n"}, {}, {Stdout: "trunk\n"}, {ExitCode: 1}, {ExitCode: 1}, {Stdout: "root-main\n"}, {Stdout: "child-trunk\n"}, {Stdout: "160000 commit child-trunk\tapi\n"}}}
	if _, err := PlanWorktree(context.Background(), runner, []WorktreeTarget{root, child}, destination, "feature/demo"); err != nil {
		t.Fatal(err)
	}
	for _, call := range runner.calls {
		if len(call.args) > 1 && call.args[0] == "ls-tree" && call.args[1] == "HEAD" {
			t.Fatalf("gitlink used HEAD: %v", runner.calls)
		}
	}
}

func TestPlanWorktreeRefusesDirtyRepositoryBeforeAnyCreation(t *testing.T) {
	runner := &recordingRunner{results: []Result{
		{}, {Stdout: "true\n"}, {Stdout: " M README.md\n"}, {Stdout: "main\n"},
	}}
	_, err := PlanWorktree(context.Background(), runner, []WorktreeTarget{{
		Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, Path: ".",
	}}, filepath.Join(t.TempDir(), "feature"), "feature/demo")
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("PlanWorktree() error = %v, want dirty preflight refusal", err)
	}
	for _, call := range runner.calls {
		if len(call.args) > 0 && call.args[0] == "worktree" {
			t.Fatalf("worktree call = %#v, want no creation", call)
		}
	}
}

func TestPlanWorktreeRefusesGitlinkMismatch(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "feature-workspace")
	root := WorktreeTarget{Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, Path: "."}
	web := WorktreeTarget{Repository: Repository{ID: "web", Dir: "/workspace/root/web-app"}, Path: "web-app"}
	runner := &recordingRunner{results: []Result{
		{},
		{Stdout: "true\n"}, {}, {Stdout: "main\n"},
		{Stdout: "true\n"}, {}, {Stdout: "main\n"},
		{ExitCode: 1}, {ExitCode: 1},
		{Stdout: "root-sha\n"}, {Stdout: "child-sha\n"},
		{Stdout: "160000 commit other-sha\tweb-app\n"},
	}}
	_, err := PlanWorktree(context.Background(), runner, []WorktreeTarget{root, web}, destination, "feature/demo")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("PlanWorktree() error = %v, want gitlink mismatch", err)
	}
}

func TestExecuteWorktreeCreatesRootThenNestedChildren(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "feature-workspace")
	root := WorktreeStep{Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, Destination: destination, Branch: "feature/demo", StartPoint: "HEAD"}
	web := WorktreeStep{Repository: Repository{ID: "web", Dir: "/workspace/root/web-app"}, Destination: filepath.Join(destination, "web-app"), Branch: "feature/demo", StartPoint: "HEAD"}
	runner := &recordingRunner{results: []Result{{}, {}}}

	report, err := ExecuteWorktree(context.Background(), runner, WorktreePlan{Steps: []WorktreeStep{root, web}}, false)
	if err != nil {
		t.Fatalf("ExecuteWorktree() error = %v", err)
	}
	if !reflect.DeepEqual(report.Created, []WorktreeStep{root, web}) {
		t.Fatalf("created = %#v, want root then web", report.Created)
	}
	wantCalls := []recordedCall{
		{dir: root.Repository.Dir, args: []string{"worktree", "add", "-b", "feature/demo", root.Destination, "HEAD"}},
		{dir: web.Repository.Dir, args: []string{"worktree", "add", "-b", "feature/demo", web.Destination, "HEAD"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestExecuteWorktreeDryRunDoesNotInvokeGit(t *testing.T) {
	step := WorktreeStep{Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, Destination: filepath.Join(t.TempDir(), "feature"), Branch: "feature/demo", StartPoint: "HEAD"}
	runner := &recordingRunner{}
	report, err := ExecuteWorktree(context.Background(), runner, WorktreePlan{Steps: []WorktreeStep{step}}, true)
	if err != nil {
		t.Fatalf("ExecuteWorktree() error = %v", err)
	}
	if !report.DryRun || !reflect.DeepEqual(report.Planned, []WorktreeStep{step}) {
		t.Fatalf("report = %#v, want dry-run plan", report)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v, want no Git invocation", runner.calls)
	}
}
