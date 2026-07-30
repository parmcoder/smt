package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type recordedCall struct {
	dir  string
	args []string
}

type recordingRunner struct {
	calls    []recordedCall
	results  []Result
	errors   []error
	nextCall int
}

func (r *recordingRunner) Run(_ context.Context, dir string, args ...string) (Result, error) {
	r.calls = append(r.calls, recordedCall{dir: dir, args: append([]string(nil), args...)})
	index := r.nextCall
	r.nextCall++
	if index >= len(r.results) {
		return Result{}, nil
	}
	var err error
	if index < len(r.errors) {
		err = r.errors[index]
	}
	return r.results[index], err
}

func TestInspectorUsesExactGitArguments(t *testing.T) {
	runner := &recordingRunner{results: []Result{
		{Stdout: "true\n"},
		{Stdout: " M tracked.txt\n?? new.txt\n"},
		{Stdout: "feature/demo\n"},
	}}
	repository := Repository{ID: "repo", Dir: "/workspace/repo", IsRoot: true}

	state, err := Inspect(context.Background(), runner, repository)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	wantState := State{
		Branch:       "feature/demo",
		Dirty:        true,
		Initialized:  true,
		ChangedFiles: []string{"tracked.txt", "new.txt"},
	}
	if !reflect.DeepEqual(state, wantState) {
		t.Fatalf("State = %#v, want %#v", state, wantState)
	}
	wantCalls := []recordedCall{
		{dir: "/workspace/repo", args: []string{"rev-parse", "--is-inside-work-tree"}},
		{dir: "/workspace/repo", args: []string{"status", "--porcelain=v1", "--untracked-files=all"}},
		{dir: "/workspace/repo", args: []string{"symbolic-ref", "--quiet", "--short", "HEAD"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestInspectorIsWorktreeUsesExactGitArguments(t *testing.T) {
	runner := &recordingRunner{results: []Result{{Stdout: "true\n"}}}
	worktree, err := (Inspector{Runner: runner}).IsWorktree(context.Background(), "/workspace/repo")
	if err != nil {
		t.Fatalf("IsWorktree() error = %v", err)
	}
	if !worktree {
		t.Fatal("IsWorktree() = false, want true")
	}
	wantCalls := []recordedCall{{dir: "/workspace/repo", args: []string{"rev-parse", "--is-inside-work-tree"}}}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestInspectorTreatsSymbolicRefFailureAsDetached(t *testing.T) {
	runner := &recordingRunner{results: []Result{
		{Stdout: "true\n"},
		{},
		{ExitCode: 1, Stderr: "fatal: ref HEAD is not a symbolic ref\n"},
	}}
	state, err := Inspect(context.Background(), runner, Repository{ID: "repo", Dir: "/workspace/repo"})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !state.Initialized || !state.Detached || state.Branch != "" {
		t.Fatalf("State = %#v, want initialized detached state", state)
	}
}

func TestChangedFilesUsesUnstagedStagedAndUntrackedArguments(t *testing.T) {
	runner := &recordingRunner{results: []Result{
		{Stdout: "worktree.txt\nshared.txt\n"},
		{Stdout: "staged.txt\nshared.txt\n"},
		{Stdout: "untracked.txt\n"},
	}}
	repository := Repository{ID: "repo", Dir: "/workspace/repo"}

	files, err := ChangedFiles(context.Background(), runner, repository)
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	wantFiles := []string{"shared.txt", "staged.txt", "untracked.txt", "worktree.txt"}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Fatalf("files = %#v, want %#v", files, wantFiles)
	}
	wantCalls := []recordedCall{
		{dir: "/workspace/repo", args: []string{"diff", "--name-only", "--diff-filter=ACMRTUXB"}},
		{dir: "/workspace/repo", args: []string{"diff", "--cached", "--name-only", "--diff-filter=ACMRTUXB"}},
		{dir: "/workspace/repo", args: []string{"ls-files", "--others", "--exclude-standard"}},
	}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestCommitMessagesUsesRangeArgumentAndParsesFullMessages(t *testing.T) {
	runner := &recordingRunner{results: []Result{{Stdout: "abc123\x00feat(api): add API\n\nBody.\n\nRefs: #1\n\x00def456\x00fix(web): fix UI\n\nBody.\n\x00"}}}
	repository := Repository{ID: "repo", Dir: "/workspace/repo"}

	messages, err := CommitMessages(context.Background(), runner, repository, "base", "head")
	if err != nil {
		t.Fatalf("CommitMessages() error = %v", err)
	}
	want := []CommitMessage{
		{SHA: "abc123", Message: "feat(api): add API\n\nBody.\n\nRefs: #1"},
		{SHA: "def456", Message: "fix(web): fix UI\n\nBody."},
	}
	if !reflect.DeepEqual(messages, want) {
		t.Fatalf("messages = %#v, want %#v", messages, want)
	}
	wantCalls := []recordedCall{{dir: "/workspace/repo", args: []string{"log", "--format=%H%x00%B%x00", "base..head", "--"}}}
	if !reflect.DeepEqual(runner.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", runner.calls, wantCalls)
	}
}

func TestTemporaryRepositoryInspection(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "smt@example.invalid")
	runGit(t, dir, "config", "user.name", "SMT Test")
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial")

	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("untracked\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("ignored\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := Inspect(context.Background(), ExecRunner{}, Repository{ID: "repo", Dir: dir, IsRoot: true})
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if !state.Initialized || !state.Dirty || state.Detached {
		t.Fatalf("State = %#v, want initialized dirty attached state", state)
	}
	files, err := ChangedFiles(context.Background(), ExecRunner{}, Repository{ID: "repo", Dir: dir})
	if err != nil {
		t.Fatalf("ChangedFiles() error = %v", err)
	}
	sort.Strings(files)
	if !reflect.DeepEqual(files, []string{"staged.txt", "tracked.txt", "untracked.txt"}) {
		t.Fatalf("files = %#v, want tracked, staged, and untracked files only", files)
	}
	for _, file := range files {
		if strings.Contains(file, "ignored") {
			t.Fatalf("ignored file included in changed files: %#v", files)
		}
	}

	runGit(t, dir, "checkout", "--detach", "HEAD")
	detached, err := Inspect(context.Background(), ExecRunner{}, Repository{ID: "repo", Dir: dir})
	if err != nil {
		t.Fatalf("Inspect() detached error = %v", err)
	}
	if !detached.Detached || detached.Branch != "" {
		t.Fatalf("State = %#v, want detached state", detached)
	}
}

func TestInspectIgnoresOnlyUntrackedOSMetadata(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "smt@example.invalid")
	runGit(t, dir, "config", "user.name", "SMT Test")
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "tracked.txt")
	runGit(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("metadata\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := Inspect(context.Background(), ExecRunner{}, Repository{ID: "repo", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if state.Dirty || len(state.ChangedFiles) != 0 {
		t.Fatalf("state = %#v, want harmless untracked metadata ignored", state)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err = Inspect(context.Background(), ExecRunner{}, Repository{ID: "repo", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Dirty || len(state.ChangedFiles) != 1 || state.ChangedFiles[0] != "tracked.txt" {
		t.Fatalf("state = %#v, want tracked change to remain visible", state)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
	return string(output)
}
