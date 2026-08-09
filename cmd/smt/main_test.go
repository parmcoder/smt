package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	applypkg "github.com/parmcoder/smt/internal/apply"
	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/hooks"
	"github.com/parmcoder/smt/internal/scaffold"
	"gopkg.in/yaml.v3"
)

func TestRunApplyParsesConfigWithoutPrompting(t *testing.T) {
	original := newApplyService
	t.Cleanup(func() { newApplyService = original })
	called := 0
	newApplyService = func() applypkg.Service {
		return applypkg.Service{Prerequisites: applyPrereq(func(context.Context) error { called++; return errors.New("stop") }), Beads: applyInit(func(context.Context, string) error { return nil })}
	}
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("smt.yaml", applyBlueprint(), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput([]string{"apply", filepath.Join(root, "workspace")}, strings.NewReader("should not be read"), out, errOut); code != exitValidation || called != 1 {
		t.Fatalf("code=%d called=%d stdout=%q stderr=%q", code, called, out.String(), errOut.String())
	}
}

func TestRunApplyUsesCustomConfigAndLeafUsage(t *testing.T) {
	for name, args := range map[string][]string{"missing path": {"apply"}, "extra path": {"apply", "a", "b"}, "bad flag": {"apply", "--unknown", "a"}} {
		t.Run(name, func(t *testing.T) {
			out, errOut := new(strings.Builder), new(strings.Builder)
			if code := run(args, out, errOut); code != exitUsage || !strings.Contains(errOut.String(), "Usage:\n  smt apply PATH") || strings.Count(errOut.String(), "Usage:") != 1 {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
		})
	}
	original := newApplyService
	t.Cleanup(func() { newApplyService = original })
	newApplyService = func() applypkg.Service {
		return applypkg.Service{Prerequisites: applyPrereq(func(context.Context) error { return errors.New("stop") }), Beads: applyInit(func(context.Context, string) error { return nil })}
	}
	root := t.TempDir()
	custom := filepath.Join(root, "custom.yaml")
	if err := os.WriteFile(custom, applyBlueprint(), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"apply", "--config", custom, filepath.Join(root, "workspace")}, out, errOut); code != exitValidation || !strings.Contains(errOut.String(), "apply prerequisites") {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

type applyPrereq func(context.Context) error

func (f applyPrereq) Check(ctx context.Context) error { return f(ctx) }

type applyInit func(context.Context, string) error

func (f applyInit) Initialize(ctx context.Context, path string) error { return f(ctx, path) }

type noReadReader struct{}

func (noReadReader) Read([]byte) (int, error) { panic("unexpected stdin read") }
func applyBlueprint() []byte {
	return []byte(`version: 1
workspace: {ai_assist: codex, stack: {web: nextjs}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, web]}
repositories: [{id: repo, path: ., scope: repo, remote: {url: ""}}, {id: web, path: web-app, component: web, technology: nextjs, scope: web, remote: {url: ""}}]
workflow: {policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}, plugins: [{source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}, {source: parmcoder/godex, selectors: [godex-go-backend]}]}
`)
}

func TestRunValidateMessageExitCodes(t *testing.T) {
	tests := []struct {
		name       string
		message    string
		wantCode   int
		wantOutput string
	}{
		{name: "valid", message: "feat(api): add a thing\n", wantCode: 0, wantOutput: "valid commit message"},
		{name: "invalid", message: "feat: add a thing\n", wantCode: 2, wantOutput: "scope is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := writeTestConfig("smt.yaml"); err != nil {
				t.Fatal(err)
			}
			file := t.TempDir() + "/message"
			if err := writeTestMessage(file, tt.message); err != nil {
				t.Fatal(err)
			}
			out, errOut := new(strings.Builder), new(strings.Builder)
			code := run([]string{"validate-message", file}, out, errOut)
			if code != tt.wantCode {
				t.Fatalf("run() code = %d, want %d; stderr=%q", code, tt.wantCode, errOut.String())
			}
			if !strings.Contains(out.String()+errOut.String(), tt.wantOutput) {
				t.Fatalf("output = %q, want substring %q", out.String()+errOut.String(), tt.wantOutput)
			}
		})
	}
}

func TestRunInitPrintsWriteFreeMigrationGuidance(t *testing.T) {
	t.Chdir(t.TempDir())
	destination := filepath.Join(t.TempDir(), "platform")
	for _, args := range [][]string{{"init"}, {"init", destination}} {
		out, errOut := new(strings.Builder), new(strings.Builder)
		code := runWithInput(args, noReadReader{}, out, errOut)
		if code != exitOK || out.String() != "smt init no longer creates a workspace; run smt new [FILE], review smt.yaml, then run smt apply [--config FILE] PATH\n" || errOut.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination=%v", err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput([]string{"init", "one", "two"}, noReadReader{}, out, errOut); code != exitUsage || errOut.String() != "usage: smt init [PATH]\n" {
		t.Fatalf("code=%d stderr=%q", code, errOut.String())
	}
}

type fakeAgentService struct {
	ready   []beads.Issue
	reviews []beads.Issue
	release beads.ReleaseReadiness
	queue   beads.QueueResult
	requeue beads.Recovery
	err     error
}

func (f fakeAgentService) ReadyWork(context.Context) ([]beads.Issue, error) { return f.ready, f.err }
func (f fakeAgentService) ListReviews(context.Context) ([]beads.Issue, error) {
	return f.reviews, f.err
}
func (f fakeAgentService) QueueReview(context.Context, string, string, string) (beads.QueueResult, error) {
	return f.queue, f.err
}
func (f fakeAgentService) RequeueAfterFix(context.Context, string) (beads.Recovery, error) {
	return f.requeue, f.err
}
func (f fakeAgentService) ReleaseReadiness(context.Context) (beads.ReleaseReadiness, error) {
	return f.release, f.err
}

func TestAgentRoutesUseSafeDeterministicDTOsAndExactUsage(t *testing.T) {
	original := newBeadsService
	t.Cleanup(func() { newBeadsService = original })
	fake := fakeAgentService{ready: []beads.Issue{{ID: "feat-1", Title: "Feature", Status: "open", Type: "feature", Labels: []string{"internal"}}}, reviews: []beads.Issue{{ID: "review-1", Title: "Review", Status: "open", Type: "task", ReviewState: "queued"}}, queue: beads.QueueResult{FeatureID: "feat-1", ReviewID: "review-1"}, requeue: beads.Recovery{ReviewID: "review-1", BugID: "bug-1", Recovery: "retry"}, release: beads.ReleaseReadiness{Ready: false, Blocking: []beads.Issue{{ID: "review-1", Status: "open"}}}}
	newBeadsService = func(string) agentService { return fake }
	for _, tc := range []struct {
		args     []string
		code     int
		out, err string
	}{
		{[]string{"work", "ready"}, exitOK, "feat-1 open feature state= Feature\n", ""},
		{[]string{"work", "ready", "--json"}, exitOK, `[{"id":"feat-1","title":"Feature","status":"open","type":"feature","labels":["internal"]}]` + "\n", ""},
		{[]string{"review", "list", "--json"}, exitOK, `[{"id":"review-1","title":"Review","status":"open","type":"task","labels":null,"review_state":"queued"}]` + "\n", ""},
		{[]string{"review", "queue", "feat-1", "--handoff", "docs/h.md", "--evidence", "docs/e.md", "--json"}, exitOK, `{"review_id":"review-1"}` + "\n", ""},
		{[]string{"review", "requeue", "review-1", "--json"}, exitOK, `{"review_id":"review-1","bug_id":"bug-1","recovery":"retry"}` + "\n", ""},
		{[]string{"release", "check", "--json"}, exitValidation, `{"ready":false,"blocking":[{"id":"review-1","title":"","status":"open","type":"","labels":null}]}` + "\n", ""},
		{[]string{"review", "requeue", "review-1", "extra"}, exitUsage, "", "usage: smt review requeue REVIEW [--json]\n"},
		{[]string{"review", "queue", "feat-1", "--handoff", "docs/h.md", "unexpected"}, exitUsage, "", "usage: smt review queue FEATURE --handoff PATH --evidence PATH [--json]\n"},
	} {
		out, errOut := new(strings.Builder), new(strings.Builder)
		code := runAgentRoute(context.Background(), tc.args, "/unused", out, errOut)
		if code != tc.code || out.String() != tc.out || errOut.String() != tc.err {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", tc.args, code, out.String(), errOut.String())
		}
	}
	called := 0
	newBeadsService = func(string) agentService { called++; return fake }
	for _, args := range [][]string{{"review", "pass"}, {"review", "pass", "x"}, {"review", "fail"}, {"review", "fail", "x"}, {"review", "close"}, {"review", "close", "x"}} {
		out, errOut := new(strings.Builder), new(strings.Builder)
		if code := runWithInput(args, strings.NewReader(""), out, errOut); code != exitUsage || !strings.Contains(errOut.String(), "human review decisions") {
			t.Fatalf("args=%v code=%d stderr=%q", args, code, errOut.String())
		}
	}
	if called != 0 {
		t.Fatalf("review mutations reached Beads service %d times", called)
	}
	for _, args := range [][]string{{"review", "queue", "feat-1", "--handoff", "docs/h.md", "--evidence", "docs/e.md", "--json"}, {"review", "requeue", "review-1", "--json"}, {"release", "check", "--json"}} {
		newBeadsService = func(string) agentService {
			return fakeAgentService{queue: beads.QueueResult{ReviewID: "review-1", Recovery: "safe retry"}, requeue: beads.Recovery{ReviewID: "review-1", Recovery: "safe retry"}, release: beads.ReleaseReadiness{}, err: errors.New("token=secret")}
		}
		out, errOut := new(strings.Builder), new(strings.Builder)
		if code := runAgentRoute(context.Background(), args, "/unused", out, errOut); code != exitValidation || strings.Contains(out.String()+errOut.String(), "secret") || !strings.Contains(errOut.String(), "operation failed") {
			t.Fatalf("failure args=%v code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}
func TestRunBareHelpAndReviewTTYRouting(t *testing.T) {
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput(nil, strings.NewReader(""), out, errOut); code != exitOK || errOut.Len() != 0 || !strings.Contains(out.String(), "Getting Started") {
		t.Fatalf("bare code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
	oldTTY, oldTUI := reviewIsInteractive, runReviewTUI
	t.Cleanup(func() { reviewIsInteractive, runReviewTUI = oldTTY, oldTUI })
	reviewIsInteractive = func(io.Reader, io.Writer) bool { return false }
	out.Reset()
	errOut.Reset()
	if code := runWithInput([]string{"review"}, strings.NewReader(""), out, errOut); code != exitUsage || errOut.String() != "review: interactive terminal input and output are required\n" {
		t.Fatalf("review code=%d stderr=%q", code, errOut.String())
	}
	calls := 0
	var gotNoColor bool
	var gotRoot string
	wantRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	reviewIsInteractive = func(io.Reader, io.Writer) bool { return true }
	t.Setenv("NO_COLOR", "1")
	runReviewTUI = func(_ context.Context, noColor bool, root string) error {
		calls++
		gotNoColor, gotRoot = noColor, root
		return nil
	}
	out.Reset()
	errOut.Reset()
	if code := runWithInput([]string{"review"}, strings.NewReader(""), out, errOut); code != exitOK || calls != 1 || !gotNoColor || gotRoot != wantRoot {
		t.Fatalf("interactive code=%d calls=%d noColor=%t root=%q", code, calls, gotNoColor, gotRoot)
	}
	runReviewTUI = func(context.Context, bool, string) error { return errors.New("private TUI error") }
	out.Reset()
	errOut.Reset()
	if code := runWithInput([]string{"review"}, strings.NewReader(""), out, errOut); code != exitInternal || errOut.String() != "review: terminal interface failed\n" || strings.Contains(errOut.String(), "private TUI error") {
		t.Fatalf("TUI error code=%d stderr=%q", code, errOut.String())
	}
}

const cobraRootHelpGolden = `Sanovy Mono Tool

Usage:
  smt [flags]
  smt [command]

Getting Started
  apply            Apply a workspace blueprint
  init             Show workspace initialization guidance
  new              Create a workspace blueprint

Workspace
  doctor           Check local readiness
  push             Push configured repositories
  status           Show workspace status
  worktree         Manage linked worktrees

Review Workflow
  release          Check release readiness
  review           Open the review terminal interface
  work             Manage work items

Developer Tools
  check            Run a check profile
  ci               Run CI-parity tools
  completion       Generate the autocompletion script for the specified shell
  contracts        Inspect reusable contracts
  help             Help about any command
  validate-message Validate a commit message

Flags:
  -h, --help      help for smt
      --verbose   write diagnostic command details to stderr

Use "smt [command] --help" for more information about a command.
`

func TestCobraRootHelpMatchesGoldenWithoutConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput(nil, strings.NewReader(""), out, errOut); code != exitOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if out.String() != cobraRootHelpGolden || errOut.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestCobraHelpAliasesWriteStdoutWithoutConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"help"}, {"--help"}} {
		out, errOut := new(strings.Builder), new(strings.Builder)
		if code := runWithInput(args, strings.NewReader(""), out, errOut); code != exitOK {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
		if out.Len() == 0 || errOut.Len() != 0 {
			t.Fatalf("args=%q stdout=%q stderr=%q", args, out.String(), errOut.String())
		}
	}
}

func TestCobraCompletionDoesNotLoadConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput([]string{"completion", "zsh"}, strings.NewReader(""), out, errOut); code != exitOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "#compdef smt") || errOut.Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestCobraSyntaxErrorsAreConciseAndUseStderr(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, args := range [][]string{{"stattus"}, {"init", "one", "two"}} {
		out, errOut := new(strings.Builder), new(strings.Builder)
		if code := runWithInput(args, strings.NewReader(""), out, errOut); code != exitUsage {
			t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
		usageCount := strings.Count(errOut.String(), "Usage:") + strings.Count(errOut.String(), "usage: smt")
		if out.Len() != 0 || usageCount != 1 {
			t.Fatalf("args=%q stdout=%q stderr=%q", args, out.String(), errOut.String())
		}
	}
}

func TestCobraMigratedLeavesRejectInvalidSyntaxBeforeConfig(t *testing.T) {
	t.Chdir(t.TempDir())
	for _, tc := range []struct {
		name string
		args []string
		use  string
	}{
		{name: "new has at most one path", args: []string{"new", "one", "two"}, use: "new [FILE]"},
		{name: "apply needs path", args: []string{"apply"}, use: "apply PATH"},
		{name: "status has no args", args: []string{"status", "extra"}, use: "status"},
		{name: "doctor has no args", args: []string{"doctor", "extra"}, use: "doctor"},
		{name: "check needs profile", args: []string{"check"}, use: "check"},
		{name: "check profile is non-empty", args: []string{"check", "--profile", ""}, use: "check"},
		{name: "push has no args", args: []string{"push", "extra"}, use: "push"},
		{name: "status rejects unknown flags", args: []string{"status", "--unknown"}, use: "status"},
		{name: "worktree add needs branch", args: []string{"worktree", "add", "destination"}, use: "worktree add PATH"},
		{name: "worktree branch is non-empty", args: []string{"worktree", "add", "destination", "--branch", ""}, use: "worktree add PATH"},
		{name: "contracts validate has no args", args: []string{"contracts", "validate", "extra"}, use: "contracts validate"},
		{name: "ci audit has no args", args: []string{"ci", "audit", "extra"}, use: "ci audit"},
		{name: "ci contracts bump needs id", args: []string{"ci", "contracts", "bump"}, use: "ci contracts bump"},
		{name: "ci contracts bump id is non-empty", args: []string{"ci", "contracts", "bump", "--id", ""}, use: "ci contracts bump"},
		{name: "validate message needs file", args: []string{"validate-message"}, use: "validate-message FILE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut := new(strings.Builder), new(strings.Builder)
			if code := runWithInput(tc.args, strings.NewReader(""), out, errOut); code != exitUsage {
				t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
			}
			if out.Len() != 0 || strings.Contains(errOut.String(), "configuration error") {
				t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
			}
			if !strings.Contains(errOut.String(), "Usage:\n  smt "+tc.use) || strings.Count(errOut.String(), "Usage:") != 1 {
				t.Fatalf("stderr=%q, want one leaf usage for %q", errOut.String(), tc.use)
			}
		})
	}
}

func TestCobraMigratedLeafHelpAndValidOutput(t *testing.T) {
	t.Chdir(t.TempDir())
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput([]string{"apply", "--help"}, strings.NewReader(""), out, errOut); code != exitOK {
		t.Fatalf("help code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	for _, want := range []string{"Apply a workspace blueprint", "Usage:\n  smt apply PATH", "--config string"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("help stdout=%q, want %q", out.String(), want)
		}
	}
	if errOut.Len() != 0 {
		t.Fatalf("help stderr=%q", errOut.String())
	}

	if err := writeTestConfig("smt.yaml"); err != nil {
		t.Fatal(err)
	}
	message := filepath.Join(t.TempDir(), "message")
	if err := writeTestMessage(message, "feat(api): add a thing\n"); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	if code := runWithInput([]string{"validate-message", message}, strings.NewReader(""), out, errOut); code != exitOK || out.String() != "valid commit message\n" || errOut.Len() != 0 {
		t.Fatalf("valid code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
}

func TestCobraPersistentVerbosePreservesLeafSuccess(t *testing.T) {
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput([]string{"init", "--verbose"}, strings.NewReader(""), out, errOut); code != exitOK {
		t.Fatalf("code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "smt init no longer creates a workspace") || !strings.Contains(errOut.String(), "command finished") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestRunNewCreatesConfigurationWithoutExistingConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	allowNewInput(t)
	out, errOut := new(strings.Builder), new(strings.Builder)
	code := runWithInput([]string{"new"}, strings.NewReader("\n\n\n\ny\n"), out, errOut)
	if code != exitOK {
		t.Fatalf("run new code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	if _, err := config.Load("smt.yaml"); err != nil {
		t.Fatalf("generated smt.yaml load: %v", err)
	}
}

func TestRunNewCreatesConfigurationAtCustomPath(t *testing.T) {
	t.Chdir(t.TempDir())
	allowNewInput(t)
	destination := filepath.Join(t.TempDir(), "custom.yaml")
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput([]string{"new", destination}, strings.NewReader("n\ny\nn\nn\ny\n"), out, errOut); code != exitOK {
		t.Fatalf("run new code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	if _, err := config.Load(destination); err != nil {
		t.Fatalf("custom generated smt.yaml load: %v", err)
	}
}

func TestRunNewUsageAndDecline(t *testing.T) {
	allowNewInput(t)
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput([]string{"new", "a", "b"}, strings.NewReader(""), out, errOut); code != exitUsage || !strings.Contains(errOut.String(), "Usage:\n  smt new [FILE]") || strings.Count(errOut.String(), "Usage:") != 1 {
		t.Fatalf("new usage code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	destination := filepath.Join(t.TempDir(), "smt.yaml")
	out.Reset()
	errOut.Reset()
	if code := runWithInput([]string{"new", destination}, strings.NewReader("y\ny\ny\ny\nn\n"), out, errOut); code != exitOK || !strings.Contains(out.String(), "no file was written") {
		t.Fatalf("new decline code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("declined destination stat=%v, want no file", err)
	}
}

func TestRunNewRejectsNonTerminalInputWithoutWriting(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "smt.yaml")
	previous := newInputIsTerminal
	newInputIsTerminal = func(io.Reader) bool { return false }
	t.Cleanup(func() { newInputIsTerminal = previous })
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWithInput([]string{"new", destination}, strings.NewReader("y\ny\ny\ny\ny\n"), out, errOut); code != exitUsage || !strings.Contains(errOut.String(), "interactive terminal") {
		t.Fatalf("new non-terminal code=%d stdout=%q stderr=%q", code, out.String(), errOut.String())
	}
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		t.Fatalf("destination stat=%v, want no file", err)
	}
}

func allowNewInput(t *testing.T) {
	t.Helper()
	previous := newInputIsTerminal
	newInputIsTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { newInputIsTerminal = previous })
}

func TestRunPushDryRunPrintsChildFirstPlanWithoutRemoteAccess(t *testing.T) {
	root := t.TempDir()
	initTestGit(t, root)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("root\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitTestFiles(t, root, "initial")
	cfg := config.Config{Repositories: []config.Repository{{
		ID: "repo", Path: root, Remote: config.Remote{URL: "https://example.invalid/root.git"},
	}}}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runPush(context.Background(), cfg, git.ExecRunner{}, true, out, errOut); code != exitOK {
		t.Fatalf("runPush() code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "push plan") || !strings.Contains(out.String(), "repo: main") {
		t.Fatalf("stdout = %q, want root push plan", out.String())
	}
}

func TestRunWorktreeDryRunPrintsRootPlan(t *testing.T) {
	root := t.TempDir()
	initTestGit(t, root)
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("root\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitTestFiles(t, root, "initial")
	destination := filepath.Join(t.TempDir(), "feature")
	cfg := config.Config{Repositories: []config.Repository{{ID: "repo", Path: ".", Scope: "repo"}}}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := runWorktree(context.Background(), cfg, root, git.ExecRunner{}, destination, "feature/demo", true, out, errOut); code != exitOK {
		t.Fatalf("runWorktree() code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	if !strings.Contains(out.String(), "worktree plan") || !strings.Contains(out.String(), destination) {
		t.Fatalf("stdout = %q, want root worktree plan", out.String())
	}
}

func TestRunPushUsesRemoteURLsConfiguredAfterInit(t *testing.T) {
	root := filepath.Join(t.TempDir(), "platform")
	if _, err := scaffold.New(git.ExecRunner{}).Init(context.Background(), root, scaffold.Selection{Web: true, API: true, Codex: true}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	cfg, err := config.Load(filepath.Join(root, "smt.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	remoteRoot := t.TempDir()
	for index := range cfg.Repositories {
		remote := filepath.Join(remoteRoot, cfg.Repositories[index].ID+".git")
		result, err := (git.ExecRunner{}).Run(context.Background(), remoteRoot, "init", "--bare", remote)
		if err != nil || result.ExitCode != 0 {
			t.Fatalf("initialize remote %s: result=%#v error=%v", cfg.Repositories[index].ID, result, err)
		}
		cfg.Repositories[index].Remote.URL = remote
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "smt.yaml"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	commitTestFiles(t, root, "configure remotes")
	t.Chdir(root)
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"push"}, out, errOut); code != exitOK {
		t.Fatalf("run push code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	positions := []int{
		strings.Index(out.String(), "pushed web"),
		strings.Index(out.String(), "pushed api"),
		strings.Index(out.String(), "pushed repo"),
	}
	if positions[0] < 0 || positions[1] < positions[0] || positions[2] < positions[1] {
		t.Fatalf("stdout = %q, want web then api then repo", out.String())
	}
}

func TestRunVerboseWritesDiagnosticsOnlyToStderr(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := writeTestConfig("smt.yaml"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "message")
	if err := writeTestMessage(file, "feat(api): add a thing\n"); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"--verbose", "validate-message", file}, out, errOut); code != exitOK {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	if got, want := out.String(), "valid commit message\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	for _, want := range []string{
		"level=debug",
		"msg=command finished",
		"command=validate-message",
		"status=success",
		"exit_code=0",
		"duration_ms=",
		"time=",
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr = %q, want field %q", errOut.String(), want)
		}
	}
	if strings.Contains(errOut.String(), "\x1b[") {
		t.Fatalf("stderr = %q, want no ANSI colors for a buffered writer", errOut.String())
	}
	if strings.Contains(errOut.String(), file) {
		t.Fatalf("stderr contains command argument: %q", errOut.String())
	}
}

func TestNewRunLoggerColorEnvironment(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	forced := new(strings.Builder)
	newRunLogger(true, forced).Debug("forced colors")
	if !strings.Contains(forced.String(), "\x1b[") {
		t.Fatalf("forced output = %q, want ANSI colors", forced.String())
	}

	t.Setenv("NO_COLOR", "1")
	disabled := new(strings.Builder)
	newRunLogger(true, disabled).Debug("disabled colors")
	if strings.Contains(disabled.String(), "\x1b[") {
		t.Fatalf("disabled output = %q, want NO_COLOR to suppress ANSI colors", disabled.String())
	}
}

func TestRunNormalModeDoesNotWriteDebugDiagnostics(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeTestConfig("smt.yaml"); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "message")
	if err := writeTestMessage(file, "feat(api): add a thing\n"); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"validate-message", file}, out, errOut); code != exitOK {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	if errOut.Len() != 0 {
		t.Fatalf("stderr = %q, want no diagnostics", errOut.String())
	}
}

func TestRunVerboseInvalidCommandPreservesUsageAndExitCode(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := writeTestConfig("smt.yaml"); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"--verbose", "unknown"}, out, errOut); code != exitUsage {
		t.Fatalf("run() code = %d, want %d; stderr=%q", code, exitUsage, errOut.String())
	}
	if out.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", out.String())
	}
	if !strings.Contains(errOut.String(), "Usage:\n  smt") {
		t.Fatalf("stderr = %q, want usage", errOut.String())
	}
	if !strings.Contains(errOut.String(), "level=debug") ||
		!strings.Contains(errOut.String(), "msg=command finished") ||
		!strings.Contains(errOut.String(), "command=unknown") ||
		!strings.Contains(errOut.String(), "status=failed") ||
		!strings.Contains(errOut.String(), "exit_code=1") {
		t.Fatalf("stderr = %q, want invalid-command diagnostic fields", errOut.String())
	}
}

func TestRunVerboseCheckIncludesCommandResultDetails(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - kind: command
          argv: [go, version]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"--verbose", "check", "--profile", "hook"}, out, errOut); code != exitOK {
		t.Fatalf("run() code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	for _, want := range []string{
		"repository=repo",
		"profile=hook",
		"program=go",
		"status=success",
		"exit_code=0",
		"duration_ms=",
		"stderr_bytes=0",
	} {
		if !strings.Contains(errOut.String(), want) {
			t.Fatalf("stderr = %q, want field %q", errOut.String(), want)
		}
	}
}

func TestRunVerboseCheckFailureLogsSafeMetadata(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - kind: command
          argv: [go, command-that-does-not-exist]
`), 0o600); err != nil {
		t.Fatal(err)
	}

	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"--verbose", "check", "--profile", "hook"}, out, errOut); code != exitValidation {
		t.Fatalf("run() code = %d, want %d, stdout=%q, stderr=%q", code, exitValidation, out.String(), errOut.String())
	}
	if !strings.Contains(errOut.String(), "status=failed") ||
		!strings.Contains(errOut.String(), "repository=repo") ||
		!strings.Contains(errOut.String(), "profile=hook") ||
		!strings.Contains(errOut.String(), "program=go") ||
		!strings.Contains(errOut.String(), "stderr_bytes=") {
		t.Fatalf("stderr = %q, want safe failure fields", errOut.String())
	}
	for _, line := range strings.Split(errOut.String(), "\n") {
		if strings.Contains(line, "level=debug") && strings.Contains(line, "command-that-does-not-exist") {
			t.Fatalf("debug log contains full command arguments: %q", line)
		}
	}
}

func writeTestConfig(path string) error {
	return os.WriteFile(path, []byte("version: 1\ncommit:\n  types: [feat]\n  scopes: [api]\nrepositories:\n  - id: root\n    path: .\n    provider: gitlab\n    project: sanovy/root\n    scope: api\n"), 0o600)
}

func writeTestMessage(path, message string) error {
	return os.WriteFile(path, []byte(message), 0o600)
}

func TestRunStatusJSONIncludesProfilesAndContractCounts(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - kind: command
          argv: [go, test]
contracts:
  artifact:
    - id: missing
      repository: repo
      file: missing.txt
      expected: present
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"--verbose", "status", "--json"}, out, errOut); code != 0 {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	var got struct {
		Repositories []map[string]any `json:"repositories"`
		Profiles     []string         `json:"profiles"`
		Contracts    struct {
			Errors   int `json:"errors"`
			Warnings int `json:"warnings"`
		} `json:"contracts"`
	}
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("status JSON: %v\n%s", err, out.String())
	}
	if len(got.Repositories) != 1 || got.Profiles[0] != "hook" || got.Contracts.Errors != 1 {
		t.Fatalf("status JSON = %#v, want repository, hook profile, and one contract error", got)
	}
	if !strings.Contains(errOut.String(), "command=status") || !strings.Contains(errOut.String(), "status=success") {
		t.Fatalf("verbose stderr = %q, want final command result", errOut.String())
	}
}

func TestRunStatusHumanIncludesRepositoryHookState(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(testConfigYAML("gitlab", "repo", "repo")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "hooks", "commit-msg"), hooks.CommitMsgScript(), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("git", "-C", root, "add", "smt.yaml")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, output)
	}
	command = exec.Command("git", "-C", root, "-c", "core.hooksPath=/dev/null", "-c", "user.name=SMT Test", "-c", "user.email=smt@example.test", "commit", "-qm", "chore(repo): fixture")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, output)
	}

	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"status"}, out, errOut); code != 0 {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	if want := "repo: clean hook=current"; !strings.Contains(out.String(), want) {
		t.Fatalf("status output = %q, want %q", out.String(), want)
	}
}

func TestRunDoctorDoesNotRedactOrPrintTokenValue(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(testConfigYAML("gitlab", "repo", "repo")), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := "doctor-secret-must-not-appear"
	t.Setenv("SMT_GITLAB_TOKEN", secret)
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"doctor"}, out, errOut); code != 0 {
		t.Fatalf("run() code = %d, stderr=%q", code, errOut.String())
	}
	if strings.Contains(out.String()+errOut.String(), secret) {
		t.Fatalf("doctor output contains token value: %q", out.String()+errOut.String())
	}
}

func TestRunCheckRefusesMutatingProfileWithoutPermission(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	initTestGit(t, root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
    checks:
      hook:
        - kind: sql-format
          argv: [pg_format]
          include: ["**/*.sql"]
          mutates_worktree: true
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"check", "--profile", "hook"}, out, errOut); code == 0 || !strings.Contains(errOut.String(), "--allow-worktree-mutation") {
		t.Fatalf("check code = %d, stderr=%q, want mutation refusal", code, errOut.String())
	}
}

func TestRunContractsValidateReturnsValidationExitForEveryFinding(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
contracts:
  artifact:
    - id: first
      repository: repo
      file: first.txt
      expected: present
    - id: second
      repository: repo
      file: second.txt
      expected: present
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"contracts", "validate"}, out, errOut); code != exitValidation || !strings.Contains(out.String(), "first") || !strings.Contains(out.String(), "second") {
		t.Fatalf("code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunCIAuditUsesContractRulesAndValidationExit(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
contracts:
  artifact:
    - id: audit-finding
      repository: repo
      file: missing.txt
      expected: present
`), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"ci", "audit"}, out, errOut); code != exitValidation || !strings.Contains(out.String(), "audit-finding") {
		t.Fatalf("code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
}

func TestRunBumpPlanDoesNotWriteThenApplyReplaces(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := os.WriteFile("smt.yaml", []byte(`version: 1
commit:
  types: [feat]
  scopes: [repo]
repositories:
  - id: repo
    path: .
    provider: gitlab
    project: sanovy/root
    scope: repo
contracts:
  reference:
    - id: ci-pin
      repository: repo
      file: contract.txt
      expected: old
      replacement: new
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("contract.txt", []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, errOut := new(strings.Builder), new(strings.Builder)
	if code := run([]string{"ci", "contracts", "bump", "--id", "ci-pin"}, out, errOut); code != 0 || !strings.Contains(out.String(), "old") || !strings.Contains(out.String(), "new") {
		t.Fatalf("plan code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	content, _ := os.ReadFile("contract.txt")
	if string(content) != "old\n" {
		t.Fatalf("plan changed file: %q", content)
	}
	out.Reset()
	if code := run([]string{"ci", "contracts", "bump", "--id", "ci-pin", "--apply"}, out, errOut); code != 0 {
		t.Fatalf("apply code = %d, stdout=%q, stderr=%q", code, out.String(), errOut.String())
	}
	content, _ = os.ReadFile("contract.txt")
	if string(content) != "new\n" {
		t.Fatalf("apply content = %q, want replacement", content)
	}
}

func testConfigYAML(provider, id, scope string) string {
	return fmt.Sprintf("version: 1\ncommit:\n  types: [feat]\n  scopes: [%s]\nrepositories:\n  - id: %s\n    path: .\n    provider: %s\n    project: sanovy/root\n    scope: %s\n", scope, id, provider, scope)
}

func initTestGit(t *testing.T, dir string) {
	t.Helper()
	command := exec.Command("git", "-C", dir, "init", "-q")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func commitTestFiles(t *testing.T, dir, message string) {
	t.Helper()
	for _, args := range [][]string{
		{"config", "user.email", "smt@example.invalid"},
		{"config", "user.name", "SMT Test"},
		{"add", "-A"},
		{"commit", "-m", message},
	} {
		command := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
}
