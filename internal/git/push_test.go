package git

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestPlanPushPreflightsEveryRepositoryBeforeChildFirstOrder(t *testing.T) {
	root := PushTarget{Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, RemoteURL: "git@example:root.git"}
	web := PushTarget{Repository: Repository{ID: "web", Dir: "/workspace/root/web-app"}, RemoteURL: "git@example:web.git"}
	runner := &recordingRunner{results: []Result{
		{Stdout: "true\n"}, {}, {Stdout: "main\n"},
		{Stdout: "true\n"}, {}, {Stdout: "feature/demo\n"},
	}}

	plan, err := PlanPush(context.Background(), runner, []PushTarget{root, web})
	if err != nil {
		t.Fatalf("PlanPush() error = %v", err)
	}
	wantSteps := []PushStep{
		{Repository: web.Repository, Branch: "feature/demo", RemoteURL: web.RemoteURL},
		{Repository: root.Repository, Branch: "main", RemoteURL: root.RemoteURL},
	}
	if !reflect.DeepEqual(plan.Steps, wantSteps) {
		t.Fatalf("steps = %#v, want %#v", plan.Steps, wantSteps)
	}
	for _, call := range runner.calls {
		if len(call.args) > 0 && call.args[0] == "push" {
			t.Fatalf("PlanPush() performed push before plan was complete: %#v", runner.calls)
		}
	}
}

func TestPlanPushRefusesDirtyRepositoryBeforeAnyPush(t *testing.T) {
	runner := &recordingRunner{results: []Result{
		{Stdout: "true\n"}, {Stdout: " M README.md\n"}, {Stdout: "main\n"},
	}}
	_, err := PlanPush(context.Background(), runner, []PushTarget{{
		Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, RemoteURL: "git@example:root.git",
	}})
	if err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("PlanPush() error = %v, want dirty worktree refusal", err)
	}
	for _, call := range runner.calls {
		if len(call.args) > 0 && call.args[0] == "push" {
			t.Fatalf("push call = %#v, want no remote action", call)
		}
	}
}

func TestExecutePushStopsAfterPartialFailureWithoutRollback(t *testing.T) {
	web := PushStep{Repository: Repository{ID: "web", Dir: "/workspace/web"}, Branch: "feature/demo", RemoteURL: "git@example:web.git"}
	api := PushStep{Repository: Repository{ID: "api", Dir: "/workspace/api"}, Branch: "feature/demo", RemoteURL: "git@example:api.git"}
	root := PushStep{Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, Branch: "feature/demo", RemoteURL: "git@example:root.git"}
	runner := &recordingRunner{results: []Result{{}, {ExitCode: 1, Stderr: "remote rejected"}}}

	report, err := ExecutePush(context.Background(), runner, PushPlan{Steps: []PushStep{web, api, root}}, false)
	if err == nil || !strings.Contains(err.Error(), "api") || strings.Contains(err.Error(), api.RemoteURL) {
		t.Fatalf("ExecutePush() error = %v, want safe api failure", err)
	}
	if !reflect.DeepEqual(report.Pushed, []PushStep{web}) {
		t.Fatalf("pushed = %#v, want web only", report.Pushed)
	}
	if !reflect.DeepEqual(report.Pending, []PushStep{root}) {
		t.Fatalf("pending = %#v, want root only", report.Pending)
	}
	wantCalls := []recordedCall{
		{dir: web.Repository.Dir, args: []string{"push", web.RemoteURL, "HEAD:refs/heads/feature/demo"}},
		{dir: api.Repository.Dir, args: []string{"push", api.RemoteURL, "HEAD:refs/heads/feature/demo"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestExecutePushDryRunDoesNotInvokeGit(t *testing.T) {
	step := PushStep{Repository: Repository{ID: "repo", Dir: "/workspace/root", IsRoot: true}, Branch: "main", RemoteURL: "git@example:root.git"}
	runner := &recordingRunner{}
	report, err := ExecutePush(context.Background(), runner, PushPlan{Steps: []PushStep{step}}, true)
	if err != nil {
		t.Fatalf("ExecutePush() error = %v", err)
	}
	if !report.DryRun || !reflect.DeepEqual(report.Planned, []PushStep{step}) {
		t.Fatalf("report = %#v, want dry-run plan", report)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v, want no Git invocation", runner.calls)
	}
}
