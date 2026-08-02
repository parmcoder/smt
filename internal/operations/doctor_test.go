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

func TestDoctorDoesNotRequireSystemGitExecutable(t *testing.T) {
	doctor := NewDoctor(config.Config{}, func(name string) (string, error) { return "", errors.New(name + " missing") }, func(string) bool { return false }, func(context.Context, string) (git.State, error) { return git.State{}, nil })
	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Checks) != 0 {
		t.Fatalf("checks=%#v", result.Checks)
	}
}

func TestDoctorReportsRepositoryAndProviderTokenStateWithoutTokenValue(t *testing.T) {
	secret := "doctor-secret-must-not-appear"
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "missing", Path: "missing", Provider: "gitlab"},
		{ID: "github", Path: "github", Provider: "github"},
	}}
	doctor := NewDoctor(cfg, func(string) (string, error) { return "/usr/bin/tool", nil }, func(name string) bool { return name == "SMT_GITLAB_TOKEN" }, func(_ context.Context, path string) (git.State, error) {
		return git.State{Initialized: path != "missing"}, nil
	})
	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !containsDoctorCheck(result.Checks, Check{ID: "repo:missing:worktree", Status: "error", Message: "repository missing is not an initialized Git worktree"}) {
		t.Fatalf("checks=%#v", result.Checks)
	}
	if !containsDoctorCheck(result.Checks, Check{ID: "token:gitlab", Status: "ok", Message: "SMT_GITLAB_TOKEN is set"}) ||
		!containsDoctorCheck(result.Checks, Check{ID: "token:github", Status: "error", Message: "SMT_GITHUB_TOKEN is not set"}) {
		t.Fatalf("checks=%#v", result.Checks)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("doctor output contains token: %s", encoded)
	}
}

func TestDoctorDeduplicatesAndSortsProfileExecutables(t *testing.T) {
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "b", Path: "b", Profiles: config.CheckProfiles{"submit": {{Kind: "command", Argv: []string{"task", "verify"}}, {Kind: "command", Argv: []string{"go", "test"}}}}},
		{ID: "a", Path: "a", Profiles: config.CheckProfiles{"hook": {{Kind: "command", Argv: []string{"go", "fmt"}}}}},
	}}
	var lookedUp []string
	doctor := NewDoctor(cfg, func(name string) (string, error) {
		lookedUp = append(lookedUp, name)
		return "/usr/bin/" + name, nil
	}, func(string) bool { return true }, func(context.Context, string) (git.State, error) { return git.State{Initialized: true}, nil })
	if _, err := doctor.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if want := []string{"go", "task"}; !reflect.DeepEqual(lookedUp, want) {
		t.Fatalf("lookups=%v want=%v", lookedUp, want)
	}
}

func TestDoctorReportsEveryHookAndRedactsHookErrors(t *testing.T) {
	secret := "hook-secret-must-not-appear"
	cfg := config.Config{Repositories: []config.Repository{{ID: "absent", Path: "absent"}, {ID: "broken", Path: "broken"}}}
	doctor := NewDoctorWithHookInspector(cfg, nil, func(string) bool { return false }, func(context.Context, string) (git.State, error) { return git.State{Initialized: true}, nil }, func(path string) (hooks.HookStatus, error) {
		if path == "broken" {
			return "", errors.New(secret)
		}
		return hooks.HookAbsent, nil
	})
	result, err := doctor.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !containsDoctorCheck(result.Checks, Check{ID: "hook:absent:commit-msg", Status: "ok", Message: "repository absent commit-msg hook is absent"}) ||
		!containsDoctorCheck(result.Checks, Check{ID: "hook:broken:commit-msg", Status: "error", Message: "repository broken commit-msg hook could not be inspected"}) {
		t.Fatalf("checks=%#v", result.Checks)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), secret) {
		t.Fatalf("doctor output contains hook error: %s", encoded)
	}
}

func containsDoctorCheck(checks []Check, want Check) bool {
	for _, check := range checks {
		if check == want {
			return true
		}
	}
	return false
}
