package checks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

type call struct {
	dir   string
	argv  []string
	label string
}

type recordingExecutor struct {
	calls []call
	errs  []error
}

func (e *recordingExecutor) Run(_ context.Context, dir string, argv []string, label string) error {
	e.calls = append(e.calls, call{dir: dir, argv: append([]string(nil), argv...), label: label})
	index := len(e.calls) - 1
	if index < len(e.errs) {
		return e.errs[index]
	}
	return nil
}

func TestRunUsesConfiguredCommandArguments(t *testing.T) {
	repository := config.Repository{
		ID:   "api",
		Path: "apis",
		Checks: []config.Check{
			{Kind: "command", Argv: []string{"task", "verify"}},
			{Kind: "command", Argv: []string{"go", "test", "./..."}},
		},
	}
	executor := &recordingExecutor{}

	if err := Run(context.Background(), executor, repository, nil, false); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []call{
		{dir: "apis", argv: []string{"task", "verify"}, label: "api"},
		{dir: "apis", argv: []string{"go", "test", "./..."}, label: "api"},
	}
	if !reflect.DeepEqual(executor.calls, want) {
		t.Fatalf("calls = %#v, want %#v", executor.calls, want)
	}
}

func TestRunFormatsChangedSQLFilesOnly(t *testing.T) {
	repository := config.Repository{
		ID:   "database",
		Path: "database",
		Checks: []config.Check{{
			Kind:    "sql-format",
			Argv:    []string{"pg_format"},
			Include: []string{"**/*.sql"},
		}},
	}
	executor := &recordingExecutor{}

	if err := Run(context.Background(), executor, repository, []string{"schema.sql", "migrations/001.sql", "README.md", "ignored.txt"}, false); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := []call{
		{dir: "database", argv: []string{"pg_format", "-i", "schema.sql"}, label: "database"},
		{dir: "database", argv: []string{"pg_format", "-i", "migrations/001.sql"}, label: "database"},
	}
	if !reflect.DeepEqual(executor.calls, want) {
		t.Fatalf("calls = %#v, want %#v", executor.calls, want)
	}
}

func TestRunStopsAfterFormatterFailure(t *testing.T) {
	repository := config.Repository{
		ID:   "database",
		Path: "database",
		Checks: []config.Check{
			{Kind: "sql-format", Argv: []string{"pg_format"}, Include: []string{"**/*.sql"}},
			{Kind: "command", Argv: []string{"task", "verify"}},
		},
	}
	executor := &recordingExecutor{errs: []error{errors.New("formatter failed")}}

	if err := Run(context.Background(), executor, repository, []string{"schema.sql"}, false); err == nil {
		t.Fatal("Run() error = nil, want formatter failure")
	}
	if len(executor.calls) != 1 {
		t.Fatalf("calls = %#v, want only failed formatter call", executor.calls)
	}
}

func TestRunDryRunDoesNotExecute(t *testing.T) {
	repository := config.Repository{ID: "api", Path: "apis", Checks: []config.Check{{Kind: "command", Argv: []string{"task", "verify"}}}}
	executor := &recordingExecutor{}

	if err := Run(context.Background(), executor, repository, nil, true); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("calls = %#v, want no dry-run execution", executor.calls)
	}
}

func TestRunProfileSelectsNamedChecks(t *testing.T) {
	repository := config.Repository{
		ID:   "api",
		Path: "apis",
		Profiles: config.CheckProfiles{
			"hook":   {{Kind: "command", Argv: []string{"go", "test", "./hook"}}},
			"submit": {{Kind: "command", Argv: []string{"go", "test", "./submit"}}},
		},
	}
	executor := &recordingExecutor{}
	if err := RunProfile(context.Background(), executor, repository, "submit", nil, false, false); err != nil {
		t.Fatalf("RunProfile() error = %v", err)
	}
	if got := executor.calls; !reflect.DeepEqual(got, []call{{dir: "apis", argv: []string{"go", "test", "./submit"}, label: "api"}}) {
		t.Fatalf("calls = %#v, want submit profile only", got)
	}
}

func TestRunProfileRejectsMutationWithoutPermission(t *testing.T) {
	repository := config.Repository{
		ID: "api", Path: "apis", Profiles: config.CheckProfiles{
			"hook": {{Kind: "command", Argv: []string{"task", "format"}, MutatesWorktree: true}},
		},
	}
	executor := &recordingExecutor{}
	err := RunProfile(context.Background(), executor, repository, "hook", nil, false, false)
	if err == nil || !strings.Contains(err.Error(), "--allow-worktree-mutation") {
		t.Fatalf("RunProfile() error = %v, want explicit mutation guidance", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("calls = %#v, want mutation blocked before execution", executor.calls)
	}
}

func TestRunProfileDryRunDoesNotExecuteMutatingChecks(t *testing.T) {
	repository := config.Repository{ID: "db", Path: "database", Profiles: config.CheckProfiles{
		"submit": {{Kind: "sql-format", Argv: []string{"pg_format"}, Include: []string{"**/*.sql"}, MutatesWorktree: true}},
	}}
	executor := &recordingExecutor{}
	if err := RunProfile(context.Background(), executor, repository, "submit", []string{"schema.sql"}, false, true); err != nil {
		t.Fatalf("RunProfile() error = %v, want dry-run success", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("calls = %#v, want no dry-run execution", executor.calls)
	}
}
