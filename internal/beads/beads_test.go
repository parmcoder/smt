package beads

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}
type fakeRunner struct {
	calls    []call
	replies  map[string]string
	failures map[string]error
	results  map[string]ProcessResult
}

func newFake() *fakeRunner {
	return &fakeRunner{replies: map[string]string{}, failures: map[string]error{}, results: map[string]ProcessResult{}}
}
func key(args ...string) string { return strings.Join(append(args, "--json"), " ") }
func (f *fakeRunner) Run(_ context.Context, _ string, _ string, args ...string) (ProcessResult, error) {
	f.calls = append(f.calls, call{"bd", append([]string(nil), args...)})
	if err := f.failures[strings.Join(args, " ")]; err != nil {
		return f.results[strings.Join(args, " ")], err
	}
	result := f.results[strings.Join(args, " ")]
	result.Stdout = f.replies[strings.Join(args, " ")]
	return result, nil
}
func reply(f *fakeRunner, value string, args ...string) { f.replies[key(args...)] = value }
func fail(f *fakeRunner, err error, args ...string)     { f.failures[key(args...)] = err }

func TestClientRedactsRunnerAndMalformedJSON(t *testing.T) {
	f := newFake()
	fail(f, errors.New("token=secret"), "show", "missing")
	if _, err := New("/workspace", f).ShowIssue(context.Background(), "missing"); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error=%v", err)
	}
	f = newFake()
	reply(f, "[private payload", "show", "missing")
	if _, err := New("/workspace", f).ShowIssue(context.Background(), "missing"); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("error=%v", err)
	}
}

func TestClientClassifiesFailuresAndLogsOnlySafeProcessMetadata(t *testing.T) {
	f := newFake()
	f.results["show missing --json"] = ProcessResult{Stderr: "token=secret private payload", ExitCode: 1}
	var verbose bytes.Buffer
	c := New("/workspace", f)
	c.client.Verbose = &verbose
	if _, err := c.ShowIssue(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "not_found") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error=%v", err)
	}
	if strings.Contains(verbose.String(), "secret") || strings.Contains(verbose.String(), "private") || strings.Contains(verbose.String(), "show missing") {
		t.Fatalf("verbose=%q", verbose.String())
	}

	f = newFake()
	reply(f, "not-json", "show", "missing")
	if _, err := New("/workspace", f).ShowIssue(context.Background(), "missing"); err == nil || !strings.Contains(err.Error(), "invalid_json") || strings.Contains(err.Error(), "not-json") {
		t.Fatalf("error=%v", err)
	}
}

func TestNewWithVerboseAttachesSafeTelemetryWriter(t *testing.T) {
	f := newFake()
	f.results["show missing --json"] = ProcessResult{ExitCode: 1, Stderr: "secret"}
	var verbose bytes.Buffer
	_, _ = NewWithVerbose("/workspace", f, &verbose).ShowIssue(context.Background(), "missing")
	if !strings.Contains(verbose.String(), "operation=show") || !strings.Contains(verbose.String(), "classification=not_found") {
		t.Fatalf("verbose=%q", verbose.String())
	}
	if strings.Contains(verbose.String(), "secret") || strings.Contains(verbose.String(), "show missing") {
		t.Fatalf("verbose leaked sensitive data: %q", verbose.String())
	}
}

func TestEnsurePreparedWorkspaceTaskUsesOnlyActiveP2Records(t *testing.T) {
	f := newFake()
	reply(f, `[{"id":"closed","title":"Prepared workspace","status":"closed","priority":2},{"id":"active","title":"Prepared workspace","status":"in_progress","priority":2}]`, "list", "--status", "open,in_progress")
	got, err := New("/workspace", f).EnsurePreparedWorkspaceTask(context.Background())
	if err != nil || got != "active" {
		t.Fatalf("id=%q err=%v", got, err)
	}

	f = newFake()
	reply(f, `[]`, "list", "--status", "open,in_progress")
	reply(f, `{"id":"created","title":"Prepared workspace","status":"open","priority":2}`, "create", "Prepared workspace", "--type", "task", "--priority", "2")
	got, err = New("/workspace", f).EnsurePreparedWorkspaceTask(context.Background())
	if err != nil || got != "created" {
		t.Fatalf("created id=%q err=%v calls=%v replies=%v", got, err, f.calls, f.replies)
	}
}

func TestCreatePreparedWorkspaceTaskAlwaysCreatesNewTask(t *testing.T) {
	f := newFake()
	reply(f, `[{"id":"existing","title":"Prepared workspace","status":"open","priority":2}]`, "list", "--status", "open,in_progress")
	reply(f, `{"id":"created","title":"Prepared workspace","status":"open","priority":2}`, "create", "Prepared workspace", "--type", "task", "--priority", "2")
	got, err := New("/workspace", f).CreatePreparedWorkspaceTask(context.Background())
	if err != nil || got != "created" {
		t.Fatalf("id=%q err=%v calls=%v", got, err, f.calls)
	}
	if len(f.calls) != 1 || strings.Join(f.calls[0].args, " ") != "create Prepared workspace --type task --priority 2 --json" {
		t.Fatalf("calls=%v", f.calls)
	}
}
