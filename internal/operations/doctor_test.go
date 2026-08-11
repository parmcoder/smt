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
	if got, want := result.Checks[5], (Check{ID: "token:gitlab", Status: "error", Message: "SMT_GITLAB_TOKEN is not set"}); got != want {
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

func containsCheck(checks []Check, want Check) bool {
	for _, check := range checks {
		if check == want {
			return true
		}
	}
	return false
}
