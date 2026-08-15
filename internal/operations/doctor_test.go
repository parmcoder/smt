package operations

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/hooks"
)

func TestDoctorReportsMissingGitTool(t *testing.T) {
	doctor := NewDoctor(
		config.Config{},
		func(name string) (string, error) { return "", errors.New(name + " not found") },
		func(string) bool { return false },
		func(context.Context, string) (git.State, error) { return git.State{}, nil },
	)

	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := result.Checks[0], (Check{ID: "git", Status: "error", Message: "git executable is not available"}); got != want {
		t.Fatalf("git check = %#v, want %#v", got, want)
	}
}

func TestDoctorReportsBeadsDefaultAndAlignmentReadiness(t *testing.T) {
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "root", Path: "."},
		{ID: "api", Path: "api", Remote: config.Remote{DefaultBranch: "develop"}},
	}}
	doctor := NewDoctorWithBeads(cfg,
		func(string) (string, error) { return "/usr/bin/tool", nil },
		func(string) bool { return true },
		func(_ context.Context, path string) (git.State, error) {
			if path == "api" {
				return git.State{Initialized: true, Branch: "develop"}, nil
			}
			return git.State{Initialized: true, Branch: "task-7", Dirty: true}, nil
		},
		func(context.Context) error { return nil },
	)
	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	checks := make(map[string]Check, len(result.Checks))
	for _, check := range result.Checks {
		checks[check.ID] = check
	}
	if got := checks["beads:workspace"]; got.Status != DoctorStatusOK {
		t.Fatalf("beads check = %#v", got)
	}
	if got := checks["repo:api:default"]; got.Status != DoctorStatusOK || !strings.Contains(got.Message, "develop") {
		t.Fatalf("default check = %#v", got)
	}
	if got := checks["repo:root:state"]; got.Status != DoctorStatusWarning || !strings.Contains(got.Message, "clean") {
		t.Fatalf("state check = %#v", got)
	}
	if got := checks["workspace:branches"]; got.Status != DoctorStatusError || !strings.Contains(got.Message, "active Beads") {
		t.Fatalf("alignment check = %#v", got)
	}
}

func TestDoctorRejectsUnverifiedSharedNonDefaultBranch(t *testing.T) {
	cfg := config.Config{Repositories: []config.Repository{{ID: "root", Path: "."}, {ID: "api", Path: "api"}}}
	doctor := NewDoctorWithBeads(cfg,
		func(string) (string, error) { return "/usr/bin/tool", nil },
		func(string) bool { return true },
		func(context.Context, string) (git.State, error) {
			return git.State{Initialized: true, Branch: "task-7"}, nil
		},
		func(context.Context) error { return nil },
	)
	doctor.SetActiveBranchLookup(func(context.Context, string) (bool, error) { return false, nil })
	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range result.Checks {
		if check.ID == "workspace:branches" {
			if check.Status != DoctorStatusError || !strings.Contains(check.Message, "not an active Beads-ID") {
				t.Fatalf("alignment check = %#v", check)
			}
			return
		}
	}
	t.Fatal("alignment check missing")
}

func TestDoctorAlwaysChecksCoreToolsWithoutProfiles(t *testing.T) {
	var lookedUp []string
	doctor := NewDoctor(config.Config{}, func(name string) (string, error) { lookedUp = append(lookedUp, name); return "/tools/" + name, nil }, func(string) bool { return true }, func(context.Context, string) (git.State, error) { return git.State{}, nil })
	if _, err := doctor.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(lookedUp, ","), "git,smt,lefthook"; got != want {
		t.Fatalf("lookups=%q want=%q", got, want)
	}
}

func TestDoctorReportsMissingRepositoryWorktree(t *testing.T) {
	cfg := config.Config{Repositories: []config.Repository{{ID: "api", Path: "apis"}}}
	doctor := NewDoctor(
		cfg,
		func(string) (string, error) { return "/usr/bin/tool", nil },
		func(string) bool { return false },
		func(_ context.Context, path string) (git.State, error) {
			if path == "apis" {
				return git.State{}, nil
			}
			return git.State{Initialized: true}, nil
		},
	)

	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := Check{ID: "repo:api:worktree", Status: "error", Message: "repository api is not an initialized Git worktree"}
	if !reflect.DeepEqual(result.Checks[3], want) {
		t.Fatalf("repository check = %#v, want %#v", result.Checks[1], want)
	}
}

func TestDoctorReportsProviderTokenPresenceWithoutExposingValue(t *testing.T) {
	secret := "do-not-print-this-token"
	cfg := config.Config{Repositories: []config.Repository{{ID: "repo", Path: ".", Provider: "gitlab"}}}
	doctor := NewDoctor(
		cfg,
		func(string) (string, error) { return "/usr/bin/git", nil },
		func(string) bool { return false },
		func(context.Context, string) (git.State, error) { return git.State{Initialized: true}, nil },
	)

	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("json.Marshal() error = %v", marshalErr)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("result contains token value: %s", encoded)
	}
	if got, want := result.Checks[5], (Check{ID: "token:gitlab", Status: "warning", Message: "SMT_GITLAB_TOKEN is not set"}); got != want {
		t.Fatalf("token check = %#v, want %#v", got, want)
	}
}

func TestDoctorDeduplicatesConfiguredProviderAndProfileExecutables(t *testing.T) {
	profilesB := config.CheckProfiles{
		"submit": []config.Check{
			{Kind: "command", Argv: []string{"go", "test"}},
			{Kind: "command", Argv: []string{"task", "verify"}},
		},
	}
	profilesA := config.CheckProfiles{
		"hook": []config.Check{{Kind: "command", Argv: []string{"go", "fmt"}}},
	}
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "b", Path: "b", Provider: "github", Profiles: profilesB},
		{ID: "a", Path: "a", Provider: "github", Profiles: profilesA},
	}}
	var lookedUp []string
	doctor := NewDoctor(cfg, func(name string) (string, error) {
		lookedUp = append(lookedUp, name)
		return "/usr/bin/" + name, nil
	}, func(string) bool { return true }, func(context.Context, string) (git.State, error) {
		return git.State{Initialized: true}, nil
	})

	if _, err := doctor.Run(context.Background()); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []string{"git", "smt", "lefthook", "go", "task"}
	if !reflect.DeepEqual(lookedUp, want) {
		t.Fatalf("looked up = %#v, want %#v", lookedUp, want)
	}
}

func TestDoctorOrderingAndJSONAreDeterministic(t *testing.T) {
	profilesZ := config.CheckProfiles{
		"hook": []config.Check{{Kind: "command", Argv: []string{"ztool"}}},
	}
	profilesA := config.CheckProfiles{
		"hook": []config.Check{{Kind: "command", Argv: []string{"atool"}}},
	}
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "z", Path: "z", Provider: "github", Profiles: profilesZ},
		{ID: "a", Path: "a", Provider: "gitlab", Profiles: profilesA},
	}}
	doctor := NewDoctor(cfg, func(string) (string, error) { return "/usr/bin/tool", nil }, func(string) bool { return false }, func(context.Context, string) (git.State, error) {
		return git.State{Initialized: true}, nil
	})

	first, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	second, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatalf("second Run() error = %v", err)
	}
	firstJSON, _ := json.Marshal(first)
	secondJSON, _ := json.Marshal(second)
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("JSON is not deterministic: %s != %s", firstJSON, secondJSON)
	}
	if got, want := first.Checks[3].ID, "repo:z:worktree"; got != want {
		t.Fatalf("first repository check = %q, want %q", got, want)
	}
	if got, want := first.Checks[4].ID, "repo:a:worktree"; got != want {
		t.Fatalf("second repository check = %q, want %q", got, want)
	}
}

func TestDoctorReportsCommitMsgHookStateForEveryRepository(t *testing.T) {
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "absent", Path: "absent"},
		{ID: "current", Path: "current"},
		{ID: "unmanaged", Path: "unmanaged"},
	}}
	statuses := map[string]hooks.HookStatus{
		"absent":    hooks.HookAbsent,
		"current":   hooks.HookCurrent,
		"unmanaged": hooks.HookUnmanaged,
	}
	var inspected []string
	doctor := NewDoctorWithHookInspector(cfg,
		func(string) (string, error) { return "/usr/bin/git", nil },
		func(string) bool { return true },
		func(context.Context, string) (git.State, error) { return git.State{Initialized: true}, nil },
		func(repository string) (hooks.HookStatus, error) {
			inspected = append(inspected, repository)
			return statuses[repository], nil
		},
	)

	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []Check{
		{ID: "hook:absent:commit-msg", Status: "warning", Message: "repository absent commit-msg hook is absent"},
		{ID: "hook:current:commit-msg", Status: "ok", Message: "repository current commit-msg hook is current"},
		{ID: "hook:unmanaged:commit-msg", Status: "warning", Message: "repository unmanaged commit-msg hook is unmanaged"},
	}
	for _, wantCheck := range want {
		if !containsCheck(result.Checks, wantCheck) {
			t.Errorf("checks = %#v, missing %#v", result.Checks, wantCheck)
		}
	}
	if !reflect.DeepEqual(inspected, []string{"absent", "current", "unmanaged"}) {
		t.Fatalf("inspected = %#v, want every repository in order", inspected)
	}
}

func TestDoctorTreatsAbsentCommitMsgHookAsWarning(t *testing.T) {
	cfg := config.Config{Repositories: []config.Repository{{ID: "repo", Path: "."}}}
	doctor := NewDoctorWithHookInspector(cfg,
		func(string) (string, error) { return "/usr/bin/git", nil },
		func(string) bool { return true },
		func(context.Context, string) (git.State, error) { return git.State{Initialized: true}, nil },
		func(string) (hooks.HookStatus, error) { return hooks.HookAbsent, nil },
	)
	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.Checks[4], (Check{ID: "hook:repo:commit-msg", Status: "warning", Message: "repository repo commit-msg hook is absent"}); got != want {
		t.Fatalf("hook check=%#v, want=%#v", got, want)
	}
}

func TestDoctorHookInspectionIsReadOnlyAndHookErrorsAreTokenSafe(t *testing.T) {
	secret := "hook-secret-must-not-appear"
	cfg := config.Config{Repositories: []config.Repository{{ID: "repo", Path: "."}}}
	var calls int
	doctor := NewDoctorWithHookInspector(cfg,
		func(string) (string, error) { return "/usr/bin/git", nil },
		func(string) bool { return true },
		func(context.Context, string) (git.State, error) { return git.State{Initialized: true}, nil },
		func(string) (hooks.HookStatus, error) {
			calls++
			return "", errors.New(secret)
		},
	)

	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("hook inspector calls = %d, want 1", calls)
	}
	encoded, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		t.Fatalf("json.Marshal() error = %v", marshalErr)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("result contains hook error value: %s", encoded)
	}
	if got := result.Checks[4]; got.Status != "error" || got.Message != "repository repo commit-msg hook could not be inspected" {
		t.Fatalf("hook check = %#v, want token-safe inspection error", got)
	}
}

func TestBuildDoctorReportPreservesRepositoryOrderAndGroupsDerivedLeaves(t *testing.T) {
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "z", Path: "z", Remote: config.Remote{URL: "git@example.com:z.git"}, Provider: "github", Project: "org/z"},
		{ID: "local", Path: "local"},
	}}
	result := Result{Checks: []Check{
		{ID: "hook:local:commit-msg", Status: "warning", Message: "repository local commit-msg hook is unmanaged"},
		{ID: "repo:z:worktree", Status: "ok", Message: "repository z is an initialized Git worktree"},
		{ID: "repo:local:worktree", Status: "ok", Message: "repository local is an initialized Git worktree"},
		{ID: "hook:z:commit-msg", Status: "ok", Message: "repository z commit-msg hook is current"},
	}}

	report := BuildDoctorReport(cfg, result)
	if got, want := report.Status, "warning"; got != want {
		t.Fatalf("report status = %q, want %q", got, want)
	}
	if got, want := nodeIDs(report.Repositories), []string{"repo:z", "repo:local"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("repository IDs = %#v, want %#v", got, want)
	}
	if got, want := nodeIDs(report.Repositories[0].Children), []string{"repo:z:worktree", "hook:z:commit-msg", "remote:z", "provider:z"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("z children = %#v, want %#v", got, want)
	}
	if got, want := nodeIDs(report.Repositories[1].Children), []string{"repo:local:worktree", "hook:local:commit-msg", "remote:local", "provider:local"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("local children = %#v, want %#v", got, want)
	}
	if got, want := report.Repositories[0].Children[2].Status, "ok"; got != want {
		t.Fatalf("configured remote status = %q, want %q", got, want)
	}
	if got, want := report.Repositories[1].Children[2].Status, "warning"; got != want {
		t.Fatalf("absent remote status = %q, want %q", got, want)
	}
	if got, want := report.Repositories[0].Children[3].Message, "repository z provider github project org/z is configured"; got != want {
		t.Fatalf("configured provider message = %q, want %q", got, want)
	}
	if got, want := report.Repositories[1].Children[3].Message, "repository local uses local-only provider configuration"; got != want {
		t.Fatalf("local-only provider message = %q, want %q", got, want)
	}
}

func TestBuildDoctorReportDeduplicatesToolsAndCredentialsInCollectionOrder(t *testing.T) {
	result := Result{Checks: []Check{
		{ID: "tool:go", Status: "error", Message: "go executable is not available"},
		{ID: "git", Status: "ok", Message: "git executable is available"},
		{ID: "tool:go", Status: "error", Message: "go executable is not available again"},
		{ID: "token:gitlab", Status: "warning", Message: "SMT_GITLAB_TOKEN is not set"},
		{ID: "token:gitlab", Status: "warning", Message: "SMT_GITLAB_TOKEN is still not set"},
		{ID: "token:github", Status: "ok", Message: "SMT_GITHUB_TOKEN is set"},
	}}

	report := BuildDoctorReport(config.Config{}, result)
	if got, want := nodeIDs(report.Tools), []string{"tool:go", "git"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool IDs = %#v, want %#v", got, want)
	}
	if got, want := nodeIDs(report.Credentials), []string{"token:gitlab", "token:github"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("credential IDs = %#v, want %#v", got, want)
	}
}

func TestBuildDoctorReportMapsCoreErrorsAndUnmappedChecks(t *testing.T) {
	secret := "private-command-output-and-token"
	privateRemote := "https://private.example/group/project.git"
	result := Result{Checks: []Check{
		{ID: "tool:smt", Status: "error", Message: secret},
		{ID: "hook:repo:commit-msg", Status: "error", Message: secret},
		{ID: "token:gitlab", Status: "warning", Message: secret},
		{ID: "repo:missing:worktree", Status: "error", Message: secret},
		{ID: "unknown:diagnostic", Status: "error", Message: secret},
	}}
	report := BuildDoctorReport(config.Config{Repositories: []config.Repository{{
		ID: "repo", Path: ".", Remote: config.Remote{URL: privateRemote}, Provider: "gitlab", Project: "group/project",
	}}}, result)
	if got, want := report.Status, "error"; got != want {
		t.Fatalf("report status = %q, want %q", got, want)
	}
	if got, want := nodeIDs(report.Tools), []string{"tool:smt"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tool IDs = %#v, want %#v", got, want)
	}
	if got, want := nodeIDs(report.Unmapped), []string{"repo:missing:worktree", "unknown:diagnostic"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unmapped IDs = %#v, want %#v", got, want)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), secret) || strings.Contains(string(encoded), privateRemote) {
		t.Fatalf("report contains private diagnostic or remote URL: %s", encoded)
	}
}

func TestBuildDoctorReportKeepsHookVariantsAndMissingTokenAsWarning(t *testing.T) {
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "absent", Path: "absent", Provider: "gitlab", Project: "org/absent"},
		{ID: "custom", Path: "custom"},
		{ID: "current", Path: "current"},
	}}
	result := Result{Checks: []Check{
		{ID: "repo:absent:worktree", Status: "ok", Message: "repository absent is an initialized Git worktree"},
		{ID: "hook:absent:commit-msg", Status: "warning", Message: "repository absent commit-msg hook is absent"},
		{ID: "repo:custom:worktree", Status: "ok", Message: "repository custom is an initialized Git worktree"},
		{ID: "hook:custom:commit-msg", Status: "warning", Message: "repository custom commit-msg hook is unmanaged"},
		{ID: "repo:current:worktree", Status: "ok", Message: "repository current is an initialized Git worktree"},
		{ID: "hook:current:commit-msg", Status: "ok", Message: "repository current commit-msg hook is current"},
		{ID: "token:gitlab", Status: "warning", Message: "SMT_GITLAB_TOKEN is not set"},
	}}

	report := BuildDoctorReport(cfg, result)
	if got, want := report.Credentials[0].Status, "warning"; got != want {
		t.Fatalf("missing provider token status = %q, want %q", got, want)
	}
	for _, repository := range report.Repositories {
		if len(repository.Children) < 2 {
			t.Fatalf("repository %q has too few children: %#v", repository.ID, repository.Children)
		}
	}
	if got, want := report.Repositories[0].Children[1].Message, "repository absent commit-msg hook is absent"; got != want {
		t.Fatalf("absent hook message = %q, want %q", got, want)
	}
	if got, want := report.Repositories[1].Children[1].Message, "repository custom commit-msg hook is unmanaged"; got != want {
		t.Fatalf("custom hook message = %q, want %q", got, want)
	}
}

func TestBuildDoctorReportSerializationIsDeterministic(t *testing.T) {
	cfg := config.Config{Repositories: []config.Repository{{ID: "repo", Path: "."}}}
	result := Result{Checks: []Check{
		{ID: "git", Status: "ok", Message: "git executable is available"},
		{ID: "repo:repo:worktree", Status: "ok", Message: "repository repo is an initialized Git worktree"},
		{ID: "hook:repo:commit-msg", Status: "ok", Message: "repository repo commit-msg hook is current"},
	}}
	first, second := BuildDoctorReport(cfg, result), BuildDoctorReport(cfg, result)
	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("first json.Marshal() error = %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("second json.Marshal() error = %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatalf("report JSON is not deterministic: %s != %s", firstJSON, secondJSON)
	}
}

func nodeIDs(nodes []DoctorNode) []string {
	ids := make([]string, len(nodes))
	for i, node := range nodes {
		ids[i] = node.ID
	}
	return ids
}

func containsCheck(checks []Check, want Check) bool {
	for _, check := range checks {
		if check == want {
			return true
		}
	}
	return false
}
