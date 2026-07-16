package operations

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/hooks"
)

type statusCall struct {
	dir  string
	args []string
}

type statusRunner struct {
	calls   []statusCall
	results []git.Result
	errors  []error
}

func injectedHookInspector(statuses map[string]hooks.HookStatus, errors map[string]error) HookInspector {
	return func(repository string) (hooks.HookStatus, error) {
		return statuses[repository], errors[repository]
	}
}

func (r *statusRunner) Run(_ context.Context, dir string, args ...string) (git.Result, error) {
	r.calls = append(r.calls, statusCall{dir: dir, args: append([]string(nil), args...)})
	index := len(r.calls) - 1
	var result git.Result
	if index < len(r.results) {
		result = r.results[index]
	}
	var err error
	if index < len(r.errors) {
		err = r.errors[index]
	}
	return result, err
}

func TestStatusReturnsRepositoryStateInConfigOrderAndUsesExactArguments(t *testing.T) {
	runner := &statusRunner{results: []git.Result{
		{Stdout: "true\n"},
		{Stdout: " M tracked.txt\n"},
		{Stdout: "feature/demo\n"},
		{Stdout: "abc123\n"},
		{Stdout: "true\n"},
		{},
		{ExitCode: 1},
		{ExitCode: 1},
	}}
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "repo", Path: "."},
		{ID: "api", Path: "apis"},
		{ID: "database", Path: "database"},
	}}

	entries, err := NewWithHookInspector(cfg, runner, injectedHookInspector(
		map[string]hooks.HookStatus{".": hooks.HookCurrent, "apis": hooks.HookAbsent, "database": hooks.HookUnmanaged}, nil,
	)).Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	want := []Entry{
		{ID: "repo", Path: ".", Initialized: true, Dirty: true, Branch: "feature/demo", HeadSHA: "abc123", HookStatus: hooks.HookCurrent},
		{ID: "api", Path: "apis", Initialized: true, Detached: true, HookStatus: hooks.HookAbsent},
		{ID: "database", Path: "database", HookStatus: hooks.HookUnmanaged},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %#v, want %#v", entries, want)
	}
	wantCalls := []statusCall{
		{dir: ".", args: []string{"rev-parse", "--is-inside-work-tree"}},
		{dir: ".", args: []string{"status", "--porcelain=v1", "--untracked-files=all"}},
		{dir: ".", args: []string{"symbolic-ref", "--quiet", "--short", "HEAD"}},
		{dir: ".", args: []string{"rev-parse", "HEAD"}},
		{dir: "apis", args: []string{"rev-parse", "--is-inside-work-tree"}},
		{dir: "apis", args: []string{"status", "--porcelain=v1", "--untracked-files=all"}},
		{dir: "apis", args: []string{"symbolic-ref", "--quiet", "--short", "HEAD"}},
		{dir: "database", args: []string{"rev-parse", "--is-inside-work-tree"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestStatusRecordsHEADFailureAndContinues(t *testing.T) {
	runner := &statusRunner{
		results: []git.Result{
			{Stdout: "true\n"}, {}, {Stdout: "main\n"}, {ExitCode: 1, Stderr: "fatal: bad revision"},
			{Stdout: "true\n"}, {}, {Stdout: "main\n"}, {Stdout: "def456\n"},
		},
		errors: []error{nil, nil, nil, errors.New("rev-parse failed")},
	}
	cfg := config.Config{Repositories: []config.Repository{
		{ID: "first", Path: "first"},
		{ID: "second", Path: "second"},
	}}

	entries, err := NewWithHookInspector(cfg, runner, injectedHookInspector(
		map[string]hooks.HookStatus{"first": hooks.HookCurrent, "second": hooks.HookAbsent}, nil,
	)).Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if entries[0].Error == "" || entries[0].HeadSHA != "" {
		t.Fatalf("first entry = %#v, want HEAD diagnostic without SHA", entries[0])
	}
	if entries[1].HeadSHA != "def456" || entries[1].Error != "" {
		t.Fatalf("second entry = %#v, want successful state", entries[1])
	}
}

func TestStatusRecordsHookInspectionErrorAndContinues(t *testing.T) {
	runner := &statusRunner{results: []git.Result{{ExitCode: 1}, {Stdout: "true\n"}, {}, {Stdout: "main\n"}}}
	cfg := config.Config{Repositories: []config.Repository{{ID: "first", Path: "first"}, {ID: "second", Path: "second"}}}
	entries, err := NewWithHookInspector(cfg, runner, injectedHookInspector(
		map[string]hooks.HookStatus{"first": hooks.HookUnmanaged, "second": hooks.HookCurrent},
		map[string]error{"first": errors.New("hook permission denied")},
	)).Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if entries[0].HookStatus != hooks.HookUnmanaged || entries[0].Error != "hook permission denied" {
		t.Fatalf("first entry = %#v, want hook status and diagnostic", entries[0])
	}
	if entries[1].HookStatus != hooks.HookCurrent || entries[1].Error != "" {
		t.Fatalf("second entry = %#v, want successful inspection", entries[1])
	}
}

func TestStatusEntriesMarshalAsJSON(t *testing.T) {
	entries := []Entry{{ID: "repo", Path: ".", Initialized: true, HeadSHA: "abc123", Error: "diagnostic"}}
	if _, err := json.Marshal(entries); err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
}
