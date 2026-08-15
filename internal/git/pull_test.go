package git

import (
	"context"
	"strings"
	"testing"
)

func TestPlanPullPreflightsRemoteBranchesBeforeAnyUpdate(t *testing.T) {
	r := &recordingRunner{results: []Result{{Stdout: "true\n"}, {}, {Stdout: "main\n"}, {Stdout: "origin/main\n"}, {Stdout: "true\n"}, {}, {Stdout: "main\n"}, {ExitCode: 1}}}
	_, err := PlanPull(context.Background(), r, []PullTarget{{Repository: Repository{ID: "repo", Dir: "/root", IsRoot: true}, DefaultBranch: "main"}, {Repository: Repository{ID: "api", Dir: "/root/api"}, DefaultBranch: "main"}})
	if err == nil || len(r.calls) == 0 {
		t.Fatalf("err=%v calls=%v", err, r.calls)
	}
	for _, call := range r.calls {
		if len(call.args) > 0 && call.args[0] == "pull" {
			t.Fatalf("pull before preflight: %v", r.calls)
		}
	}
}

func TestExecutePullUsesFastForwardOnlyChildrenBeforeRoot(t *testing.T) {
	r := &recordingRunner{results: []Result{{}, {}}}
	plan := PullPlan{Targets: []PullTarget{{Repository: Repository{ID: "api", Dir: "/root/api"}, DefaultBranch: "main"}, {Repository: Repository{ID: "repo", Dir: "/root", IsRoot: true}, DefaultBranch: "main"}}}
	if _, err := ExecutePull(context.Background(), r, plan); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(r.calls[0].args, " "), "pull --ff-only origin main") || r.calls[1].dir != "/root" {
		t.Fatalf("calls=%v", r.calls)
	}
}

func TestPullUsesInspectedCurrentBranchNotDefaultBranch(t *testing.T) {
	r := &recordingRunner{results: []Result{{Stdout: "true\n"}, {}, {Stdout: "feature/one\n"}, {Stdout: "origin/feature/one\n"}}}
	plan, err := PlanPull(context.Background(), r, []PullTarget{{Repository: Repository{ID: "repo", Dir: "/root", IsRoot: true}, DefaultBranch: "main"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ExecutePull(context.Background(), r, plan); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.calls[len(r.calls)-1].args, " "); !strings.Contains(got, "pull --ff-only origin feature/one") {
		t.Fatalf("call=%s", got)
	}
}
