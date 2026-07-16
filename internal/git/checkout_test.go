package git

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestPlanCheckoutResolvesBranches(t *testing.T) {
	repository := Repository{ID: "repo", Dir: "/workspace/repo", IsRoot: true}
	tests := []struct {
		name      string
		results   []Result
		want      CheckoutStep
		wantCalls []recordedCall
	}{
		{
			name:    "local branch",
			results: append(inspectResults(), Result{}),
			want: CheckoutStep{
				Repository: repository,
				Branch:     "feature/demo",
				StartPoint: "feature/demo",
				Source:     Local,
			},
			wantCalls: append(inspectCalls(repository),
				recordedCall{dir: repository.Dir, args: []string{"fetch", "origin"}},
				recordedCall{dir: repository.Dir, args: []string{"show-ref", "--verify", "--quiet", "refs/heads/feature/demo"}},
			),
		},
		{
			name: "remote tracking branch",
			results: append(inspectResults(),
				Result{}, Result{ExitCode: 1}, Result{},
			),
			want: CheckoutStep{
				Repository: repository,
				Branch:     "feature/demo",
				StartPoint: "origin/feature/demo",
				Source:     Remote,
				Create:     true,
			},
			wantCalls: append(inspectCalls(repository),
				recordedCall{dir: repository.Dir, args: []string{"fetch", "origin"}},
				recordedCall{dir: repository.Dir, args: []string{"show-ref", "--verify", "--quiet", "refs/heads/feature/demo"}},
				recordedCall{dir: repository.Dir, args: []string{"show-ref", "--verify", "--quiet", "refs/remotes/origin/feature/demo"}},
			),
		},
		{
			name: "origin default branch",
			results: append(inspectResults(),
				Result{}, Result{ExitCode: 1}, Result{ExitCode: 1}, Result{Stdout: "refs/remotes/origin/trunk\n"},
			),
			want: CheckoutStep{
				Repository: repository,
				Branch:     "feature/demo",
				StartPoint: "origin/trunk",
				Source:     Default,
				Create:     true,
			},
			wantCalls: append(inspectCalls(repository),
				recordedCall{dir: repository.Dir, args: []string{"fetch", "origin"}},
				recordedCall{dir: repository.Dir, args: []string{"show-ref", "--verify", "--quiet", "refs/heads/feature/demo"}},
				recordedCall{dir: repository.Dir, args: []string{"show-ref", "--verify", "--quiet", "refs/remotes/origin/feature/demo"}},
				recordedCall{dir: repository.Dir, args: []string{"symbolic-ref", "refs/remotes/origin/HEAD"}},
			),
		},
		{
			name: "origin main fallback",
			results: append(inspectResults(),
				Result{}, Result{ExitCode: 1}, Result{ExitCode: 1}, Result{ExitCode: 1}, Result{},
			),
			want: CheckoutStep{
				Repository: repository,
				Branch:     "feature/demo",
				StartPoint: "origin/main",
				Source:     Default,
				Create:     true,
			},
			wantCalls: append(inspectCalls(repository),
				recordedCall{dir: repository.Dir, args: []string{"fetch", "origin"}},
				recordedCall{dir: repository.Dir, args: []string{"show-ref", "--verify", "--quiet", "refs/heads/feature/demo"}},
				recordedCall{dir: repository.Dir, args: []string{"show-ref", "--verify", "--quiet", "refs/remotes/origin/feature/demo"}},
				recordedCall{dir: repository.Dir, args: []string{"symbolic-ref", "refs/remotes/origin/HEAD"}},
				recordedCall{dir: repository.Dir, args: []string{"show-ref", "--verify", "--quiet", "refs/remotes/origin/main"}},
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingRunner{results: tt.results}
			steps, err := PlanCheckout(context.Background(), runner, []Repository{repository}, "feature/demo", false)
			if err != nil {
				t.Fatalf("PlanCheckout() error = %v", err)
			}
			if !reflect.DeepEqual(steps, []CheckoutStep{tt.want}) {
				t.Fatalf("steps = %#v, want %#v", steps, []CheckoutStep{tt.want})
			}
			if !reflect.DeepEqual(runner.calls, tt.wantCalls) {
				t.Fatalf("calls = %#v, want %#v", runner.calls, tt.wantCalls)
			}
		})
	}
}

func TestPlanCheckoutPreflightFailuresDoNotSwitch(t *testing.T) {
	repository := Repository{ID: "repo", Dir: "/workspace/repo"}
	tests := []struct {
		name    string
		results []Result
		errors  []error
	}{
		{name: "dirty", results: []Result{{Stdout: "true\n"}, {Stdout: " M file.txt\n"}, {Stdout: "main\n"}}},
		{name: "detached", results: []Result{{Stdout: "true\n"}, {}, {ExitCode: 1}}},
		{name: "uninitialized", results: []Result{{ExitCode: 1}}},
		{name: "fetch failure", results: append(inspectResults(), Result{ExitCode: 1}), errors: []error{nil, nil, nil, errors.New("fetch failed")}},
		{name: "resolution failure", results: append(inspectResults(), Result{}, Result{ExitCode: 1}, Result{ExitCode: 1}, Result{ExitCode: 1}, Result{ExitCode: 1})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingRunner{results: tt.results, errors: tt.errors}
			_, err := PlanCheckout(context.Background(), runner, []Repository{repository}, "feature/demo", false)
			if err == nil {
				t.Fatal("PlanCheckout() error = nil, want preflight failure")
			}
			for _, call := range runner.calls {
				if call.args[0] == "switch" {
					t.Fatalf("switch called after failure: %#v", runner.calls)
				}
			}
		})
	}
}

func TestPlanCheckoutDryRunAvoidsFetchAndSwitch(t *testing.T) {
	repository := Repository{ID: "repo", Dir: "/workspace/repo"}
	runner := &recordingRunner{results: append(inspectResults(), Result{})}

	steps, err := PlanCheckout(context.Background(), runner, []Repository{repository}, "feature/demo", true)
	if err != nil {
		t.Fatalf("PlanCheckout() error = %v", err)
	}
	if !reflect.DeepEqual(steps, []CheckoutStep{{Repository: repository, Branch: "feature/demo", StartPoint: "feature/demo", Source: Local}}) {
		t.Fatalf("steps = %#v", steps)
	}
	for _, call := range runner.calls {
		if call.args[0] == "fetch" || call.args[0] == "switch" {
			t.Fatalf("dry-run mutated repository: %#v", runner.calls)
		}
	}
}

func TestExecuteCheckoutUsesPlannedSwitchArguments(t *testing.T) {
	steps := []CheckoutStep{
		{Repository: Repository{ID: "local", Dir: "/workspace/local"}, Branch: "feature/demo", StartPoint: "feature/demo", Source: Local},
		{Repository: Repository{ID: "remote", Dir: "/workspace/remote"}, Branch: "feature/demo", StartPoint: "origin/feature/demo", Source: Remote, Create: true},
		{Repository: Repository{ID: "default", Dir: "/workspace/default"}, Branch: "feature/demo", StartPoint: "origin/main", Source: Default, Create: true},
	}
	runner := &recordingRunner{results: []Result{{}, {}, {}}}

	if err := ExecuteCheckout(context.Background(), runner, steps); err != nil {
		t.Fatalf("ExecuteCheckout() error = %v", err)
	}
	wantCalls := []recordedCall{
		{dir: "/workspace/local", args: []string{"switch", "feature/demo"}},
		{dir: "/workspace/remote", args: []string{"switch", "--track", "--create", "feature/demo", "origin/feature/demo"}},
		{dir: "/workspace/default", args: []string{"switch", "--create", "feature/demo", "origin/main"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func inspectResults() []Result {
	return []Result{{Stdout: "true\n"}, {}, {Stdout: "main\n"}}
}

func inspectCalls(repository Repository) []recordedCall {
	return []recordedCall{
		{dir: repository.Dir, args: []string{"rev-parse", "--is-inside-work-tree"}},
		{dir: repository.Dir, args: []string{"status", "--porcelain=v1", "--untracked-files=all"}},
		{dir: repository.Dir, args: []string{"symbolic-ref", "--quiet", "--short", "HEAD"}},
	}
}
