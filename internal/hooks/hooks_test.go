package hooks

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
)

type installInspector map[string]bool

func (i installInspector) IsWorktree(_ context.Context, dir string) (bool, error) { return i[dir], nil }

type installCall struct {
	dir  string
	args []string
}
type installRunner struct {
	calls []installCall
	fail  string
}

type hooksPathCall struct {
	dir  string
	args []string
}

type hooksPathRunner struct {
	calls     []hooksPathCall
	values    map[string]string
	responses map[string]hooksPathResponse
}

type hooksPathResponse struct {
	result git.Result
	err    error
}

func (r *hooksPathRunner) Run(_ context.Context, dir string, args ...string) (git.Result, error) {
	r.calls = append(r.calls, hooksPathCall{dir: dir, args: append([]string(nil), args...)})
	if response, ok := r.responses[dir]; ok {
		return response.result, response.err
	}
	if value, ok := r.values[dir]; ok {
		return git.Result{Stdout: value + "\n"}, nil
	}
	return git.Result{ExitCode: 1}, errors.New("exit status 1")
}

func lefthook210Fixture(t *testing.T) string {
	return lefthook210FixtureFile(t, "lefthook-2.1.10-commit-msg")
}

func lefthook210AssertFixture(t *testing.T) string {
	return lefthook210FixtureFile(t, "lefthook-2.1.10-assert-commit-msg")
}

func lefthook210FixtureFile(t *testing.T, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(contents), "<LEFTHOOK_PATH>", "/opt/lefthook/bin/lefthook")
}

func (r *installRunner) Run(_ context.Context, dir string, args ...string) error {
	r.calls = append(r.calls, installCall{dir: dir, args: append([]string(nil), args...)})
	if dir == r.fail {
		return os.ErrPermission
	}
	return nil
}

func TestInspectCommitMsgRecognizesLefthookDispatcher(t *testing.T) {
	repository := t.TempDir()
	hooksDir := filepath.Join(repository, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := lefthook210Fixture(t)
	if err := os.WriteFile(filepath.Join(hooksDir, "commit-msg"), []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := InspectCommitMsg(repository); err != nil || got != HookCurrent {
		t.Fatalf("InspectCommitMsg()=%q err=%v, want current", got, err)
	}
}

func TestInspectCommitMsgRecognizesAssertionEnabledLefthookDispatcher(t *testing.T) {
	repository := t.TempDir()
	hooksDir := filepath.Join(repository, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := lefthook210AssertFixture(t)
	if err := os.WriteFile(filepath.Join(hooksDir, "commit-msg"), []byte(contents), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := InspectCommitMsg(repository); err != nil || got != HookCurrent {
		t.Fatalf("InspectCommitMsg()=%q err=%v, want current", got, err)
	}
}

func TestInspectCommitMsgRejectsCustomCommandInCompleteLefthookSkeleton(t *testing.T) {
	contents := []byte(lefthook210Fixture(t))
	spoof := strings.Replace(string(contents), "\"$LEFTHOOK_BIN\" \"$@\"", "\"$LEFTHOOK_BIN\" \"$@\"\n    echo custom", 1)
	repository := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repository, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".git", "hooks", "commit-msg"), []byte(spoof), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := InspectCommitMsg(repository); err != nil || got != HookUnmanaged {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestInspectCommitMsgRejectsNonRegularHookEntries(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "symlink to managed hook",
			setup: func(t *testing.T, hookPath string) {
				t.Helper()
				target := filepath.Join(filepath.Dir(hookPath), "managed-target")
				if err := os.WriteFile(target, legacySMTCommitMsgScript(), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, hookPath); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "directory",
			setup: func(t *testing.T, hookPath string) {
				t.Helper()
				if err := os.Mkdir(hookPath, 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := t.TempDir()
			hooksDir := filepath.Join(repository, ".git", "hooks")
			if err := os.MkdirAll(hooksDir, 0o755); err != nil {
				t.Fatal(err)
			}
			test.setup(t, filepath.Join(hooksDir, "commit-msg"))
			if got, err := InspectCommitMsg(repository); err != nil || got != HookUnmanaged {
				t.Fatalf("InspectCommitMsg()=%q err=%v, want unmanaged", got, err)
			}
		})
	}
}

func TestPlanInstallPreflightsEverythingBeforeExecution(t *testing.T) {
	workspace := t.TempDir()
	repositories := []config.Repository{{ID: "repo", Path: "."}, {ID: "api", Path: "apis"}}
	for _, path := range []string{".", "apis"} {
		if err := os.MkdirAll(filepath.Join(workspace, path, ".git", "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, path, "lefthook.yml"), []byte("commit-msg:\n  commands: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "apis", ".git", "hooks", "commit-msg"), []byte("#!/bin/sh\necho custom\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	lookup := func(name string) (string, error) { return "/tools/" + name, nil }
	validator := &installRunner{}
	_, err := PlanInstall(context.Background(), workspace, repositories, lookup, installInspector{workspace: true, filepath.Join(workspace, "apis"): true}, &hooksPathRunner{}, validator)
	if err == nil || !strings.Contains(err.Error(), "api") || !strings.Contains(err.Error(), "unmanaged") {
		t.Fatalf("PlanInstall() error=%v, want unmanaged api refusal", err)
	}
}

func TestPlanInstallRejectsLegacyMigrationBackupCollision(t *testing.T) {
	workspace := t.TempDir()
	repositories := []config.Repository{{ID: "repo", Path: "."}, {ID: "api", Path: "api"}}
	for _, path := range []string{".", "api"} {
		if err := os.MkdirAll(filepath.Join(workspace, path, ".git", "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, path, "lefthook.yml"), []byte("commit-msg: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root := workspace
	child := filepath.Join(workspace, "api")
	childHook := filepath.Join(child, ".git", "hooks", "commit-msg")
	childOld := childHook + ".old"
	legacy := legacySMTCommitMsgScript()
	old := []byte("private legacy backup\n")
	if err := os.WriteFile(childHook, legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childOld, old, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := &installRunner{}
	plan, err := PlanInstall(context.Background(), workspace, repositories, func(name string) (string, error) { return name, nil }, installInspector{root: true, child: true}, &hooksPathRunner{}, runner)
	if err == nil || err.Error() != "preflight repository api: legacy hook migration collision" || strings.Contains(err.Error(), "private legacy backup") {
		t.Fatalf("PlanInstall() plan=%#v err=%v", plan, err)
	}
	if len(plan.Repositories) != 0 {
		t.Fatalf("plan=%#v, want no executable plan", plan)
	}
	for _, call := range runner.calls {
		if strings.Join(call.args, " ") != "lefthook validate" {
			t.Fatalf("unexpected hook command=%#v", call)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, ".git", "hooks", "commit-msg")); !os.IsNotExist(err) {
		t.Fatalf("root hook changed: %v", err)
	}
	for path, want := range map[string][]byte{childHook: legacy, childOld: old} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Fatalf("file %s=%q err=%v", path, got, err)
		}
	}
}

func TestPlanInstallAllowsCurrentLefthookDispatcherWithOldBackup(t *testing.T) {
	for _, test := range []struct {
		name string
		hook func(*testing.T) string
	}{
		{name: "default", hook: lefthook210Fixture},
		{name: "assertion enabled", hook: lefthook210AssertFixture},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			hooksDir := filepath.Join(workspace, ".git", "hooks")
			if err := os.MkdirAll(hooksDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(workspace, "lefthook.yml"), []byte("commit-msg: {}\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(hooksDir, "commit-msg"), []byte(test.hook(t)), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(hooksDir, "commit-msg.old"), []byte("legacy backup\n"), 0o755); err != nil {
				t.Fatal(err)
			}
			plan, err := PlanInstall(context.Background(), workspace, []config.Repository{{ID: "repo", Path: "."}}, func(name string) (string, error) { return name, nil }, installInspector{workspace: true}, &hooksPathRunner{}, &installRunner{})
			if err != nil || len(plan.Repositories) != 1 || plan.Repositories[0].ID != "repo" {
				t.Fatalf("PlanInstall() plan=%#v err=%v", plan, err)
			}
		})
	}
}

func TestPlanInstallAllowsLegacyMigrationWithoutBackup(t *testing.T) {
	workspace := t.TempDir()
	hooksDir := filepath.Join(workspace, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "lefthook.yml"), []byte("commit-msg: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "commit-msg"), legacySMTCommitMsgScript(), 0o755); err != nil {
		t.Fatal(err)
	}
	plan, err := PlanInstall(context.Background(), workspace, []config.Repository{{ID: "repo", Path: "."}}, func(name string) (string, error) { return name, nil }, installInspector{workspace: true}, &hooksPathRunner{}, &installRunner{})
	if err != nil || len(plan.Repositories) != 1 || plan.Repositories[0].ID != "repo" {
		t.Fatalf("PlanInstall() plan=%#v err=%v", plan, err)
	}
}

func TestPlanInstallTreatsLegacyBackupSymlinkAsCollision(t *testing.T) {
	workspace := t.TempDir()
	hooksDir := filepath.Join(workspace, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "lefthook.yml"), []byte("commit-msg: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hooksDir, "commit-msg"), legacySMTCommitMsgScript(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing-old-target", filepath.Join(hooksDir, "commit-msg.old")); err != nil {
		t.Fatal(err)
	}
	_, err := PlanInstall(context.Background(), workspace, []config.Repository{{ID: "repo", Path: "."}}, func(name string) (string, error) { return name, nil }, installInspector{workspace: true}, &hooksPathRunner{}, &installRunner{})
	if err == nil || err.Error() != "preflight repository repo: legacy hook migration collision" {
		t.Fatalf("PlanInstall() error=%v", err)
	}
}

func TestPlanInstallRejectsChildCoreHooksPathBeforeAnyInstall(t *testing.T) {
	workspace := t.TempDir()
	repositories := []config.Repository{{ID: "repo", Path: "."}, {ID: "api", Path: "api"}}
	for _, path := range []string{".", "api"} {
		if err := os.MkdirAll(filepath.Join(workspace, path, ".git", "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(workspace, path, "lefthook.yml"), []byte("commit-msg: {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root := workspace
	child := filepath.Join(workspace, "api")
	gitRunner := &hooksPathRunner{values: map[string]string{child: " hooks path "}}
	lefthookRunner := &installRunner{}
	_, err := PlanInstall(context.Background(), workspace, repositories, func(name string) (string, error) { return name, nil }, installInspector{root: true, child: true}, gitRunner, lefthookRunner)
	if err == nil || err.Error() != "preflight repository api: core.hooksPath is configured" || strings.Contains(err.Error(), "hooks path") {
		t.Fatalf("PlanInstall() error=%v", err)
	}
	if len(gitRunner.calls) != 2 {
		t.Fatalf("git calls=%#v", gitRunner.calls)
	}
	for i, dir := range []string{root, child} {
		call := gitRunner.calls[i]
		if call.dir != dir || strings.Join(call.args, " ") != "config --get core.hooksPath" {
			t.Fatalf("git call=%#v", call)
		}
	}
	for _, call := range lefthookRunner.calls {
		if strings.Join(call.args, " ") == "lefthook install commit-msg" {
			t.Fatalf("unexpected install call=%#v", call)
		}
	}
	if _, err := os.Lstat(filepath.Join(root, ".git", "hooks", "commit-msg")); !os.IsNotExist(err) {
		t.Fatalf("root hook changed: %v", err)
	}
}

func TestPlanInstallRejectsCoreHooksPathLookupError(t *testing.T) {
	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "lefthook.yml"), []byte("commit-msg: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRunner := &hooksPathRunner{responses: map[string]hooksPathResponse{
		workspace: {result: git.Result{ExitCode: 1, Stderr: "fatal: private hook configuration error"}, err: errors.New("exit status 1")},
	}}
	lefthookRunner := &installRunner{}
	_, err := PlanInstall(context.Background(), workspace, []config.Repository{{ID: "repo", Path: "."}}, func(name string) (string, error) { return name, nil }, installInspector{workspace: true}, gitRunner, lefthookRunner)
	if err == nil || err.Error() != "preflight repository repo: inspect core.hooksPath failed" || strings.Contains(err.Error(), "private hook") {
		t.Fatalf("PlanInstall() error=%v", err)
	}
	if len(gitRunner.calls) != 1 || strings.Join(gitRunner.calls[0].args, " ") != "config --get core.hooksPath" {
		t.Fatalf("git calls=%#v", gitRunner.calls)
	}
	if len(lefthookRunner.calls) != 0 {
		t.Fatalf("unexpected lefthook calls=%#v", lefthookRunner.calls)
	}
}

func TestPlanInstallRunsLefthookValidateForEveryRepository(t *testing.T) {
	workspace := t.TempDir()
	repositories := []config.Repository{{ID: "repo", Path: "."}, {ID: "api", Path: "api"}}
	for _, path := range []string{".", "api"} {
		if err := os.MkdirAll(filepath.Join(workspace, path, ".git", "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		contents := []byte("commit-msg: {}\n")
		if path == "api" {
			// The mapping is valid YAML but commit-msg.parallel must be boolean to Lefthook.
			contents = []byte("commit-msg:\n  parallel: definitely-not-bool\n")
		}
		if err := os.WriteFile(filepath.Join(workspace, path, "lefthook.yml"), contents, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runner := &installRunner{fail: filepath.Join(workspace, "api")}
	_, err := PlanInstall(context.Background(), workspace, repositories, func(name string) (string, error) { return name, nil }, installInspector{workspace: true, filepath.Join(workspace, "api"): true}, &hooksPathRunner{}, runner)
	if err == nil || len(runner.calls) != 2 || strings.Join(runner.calls[0].args, " ") != "lefthook validate" || strings.Join(runner.calls[1].args, " ") != "lefthook validate" {
		t.Fatalf("err=%v calls=%#v", err, runner.calls)
	}
	for _, call := range runner.calls {
		if strings.Join(call.args, " ") != "lefthook validate" {
			t.Fatalf("call=%#v, want validation only and no install", call)
		}
	}
}

func TestInspectCommitMsgRejectsSpoofedCurrentMarkers(t *testing.T) {
	for _, contents := range []string{"#!/bin/sh\n# SMT managed hook\necho custom\n", "#!/bin/sh\nLEFTHOOK=0\ncall_lefthook run \"commit-msg\" \"$@\"\n"} {
		repository := t.TempDir()
		if err := os.MkdirAll(filepath.Join(repository, ".git", "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repository, ".git", "hooks", "commit-msg"), []byte(contents), 0o755); err != nil {
			t.Fatal(err)
		}
		if got, err := InspectCommitMsg(repository); err != nil || got != HookUnmanaged {
			t.Fatalf("got=%q err=%v", got, err)
		}
	}
}

func TestInspectCommitMsgRejectsAllMarkerSpoofWithoutDispatcherStructure(t *testing.T) {
	repository := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repository, ".git", "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	spoof := "#!/bin/sh\n# LEFTHOOK\ncall_lefthook run \"commit-msg\" \"$@\"\nelif lefthook\ncall_lefthook()\nif [ \"$LEFTHOOK\" = \"0\" ]; then\n"
	if err := os.WriteFile(filepath.Join(repository, ".git", "hooks", "commit-msg"), []byte(spoof), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := InspectCommitMsg(repository); err != nil || got != HookUnmanaged {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestExecuteInstallUsesRootFirstArgumentArraysAndReportsPartialFailure(t *testing.T) {
	plan := InstallPlan{Repositories: []InstallTarget{{ID: "repo", Dir: "/workspace"}, {ID: "api", Dir: "/workspace/apis"}, {ID: "web", Dir: "/workspace/web"}}}
	runner := &installRunner{fail: "/workspace/apis"}
	report, err := ExecuteInstall(context.Background(), plan, runner, false)
	if err == nil || !strings.Contains(err.Error(), "api") || strings.Contains(err.Error(), "--force") {
		t.Fatalf("ExecuteInstall() error=%v", err)
	}
	if got, want := strings.Join(report.Installed, ","), "repo"; got != want {
		t.Fatalf("installed=%q want=%q", got, want)
	}
	if got, want := strings.Join(report.Pending, ","), "api,web"; got != want {
		t.Fatalf("pending=%q want=%q", got, want)
	}
	if len(runner.calls) != 2 || strings.Join(runner.calls[0].args, " ") != "lefthook install commit-msg" || strings.Join(runner.calls[1].args, " ") != "lefthook install commit-msg" {
		t.Fatalf("calls=%#v", runner.calls)
	}
	dryRunner := &installRunner{}
	dry, err := ExecuteInstall(context.Background(), plan, dryRunner, true)
	if err != nil || len(dryRunner.calls) != 0 || strings.Join(dry.Pending, ",") != "repo,api,web" {
		t.Fatalf("dry report=%#v err=%v calls=%#v", dry, err, dryRunner.calls)
	}
}

func TestInspectCommitMsgReportsHookStates(t *testing.T) {
	workspace := t.TempDir()
	for _, repository := range []string{"current", "unmanaged"} {
		if err := os.MkdirAll(filepath.Join(workspace, repository, ".git", "hooks"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "current", ".git", "hooks", "commit-msg"), legacySMTCommitMsgScript(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "unmanaged", ".git", "hooks", "commit-msg"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		repository string
		want       HookStatus
	}{
		{repository: "absent", want: HookAbsent},
		{repository: "current", want: HookCurrent},
		{repository: "unmanaged", want: HookUnmanaged},
	}
	for _, test := range tests {
		got, err := InspectCommitMsg(filepath.Join(workspace, test.repository))
		if err != nil {
			t.Fatalf("InspectCommitMsg(%q) error = %v", test.repository, err)
		}
		if got != test.want {
			t.Errorf("InspectCommitMsg(%q) = %q, want %q", test.repository, got, test.want)
		}
	}
}

func TestInspectCommitMsgResolvesGitdirPointer(t *testing.T) {
	workspace := t.TempDir()
	repository := filepath.Join(workspace, "repository")
	gitDir := filepath.Join(workspace, "real-gitdir")
	if err := os.MkdirAll(repository, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "hooks"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repository, ".git"), []byte("gitdir: ../real-gitdir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "hooks", "commit-msg"), legacySMTCommitMsgScript(), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := InspectCommitMsg(repository)
	if err != nil {
		t.Fatalf("InspectCommitMsg() error = %v", err)
	}
	if got != HookCurrent {
		t.Fatalf("InspectCommitMsg() = %q, want %q", got, HookCurrent)
	}
}

func TestInspectCommitMsgDoesNotMutateHook(t *testing.T) {
	repository := t.TempDir()
	hooksDir := filepath.Join(repository, ".git", "hooks")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatal(err)
	}
	hookPath := filepath.Join(hooksDir, "commit-msg")
	contents := []byte("#!/bin/sh\necho custom\n")
	if err := os.WriteFile(hookPath, contents, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := InspectCommitMsg(repository); err != nil {
		t.Fatalf("InspectCommitMsg() error = %v", err)
	}
	after, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(contents) {
		t.Fatalf("hook contents changed: %q", after)
	}
}
