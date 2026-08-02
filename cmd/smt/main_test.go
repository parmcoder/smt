package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ggit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/prereq"
	"github.com/parmcoder/smt/internal/scaffold"
	"gopkg.in/yaml.v3"
)

func TestRunRejectsRemovedWorktreeCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"worktree", "add"}, &out, &errOut); code != exitUsage {
		t.Fatalf("code=%d", code)
	}
}

func TestRunRejectsHumanReviewMutationCommands(t *testing.T) {
	for _, args := range [][]string{{"review", "pass"}, {"review", "close"}} {
		var out, errOut bytes.Buffer
		if code := run(args, &out, &errOut); code != exitUsage || !strings.Contains(errOut.String(), "usage:") {
			t.Fatalf("run(%v) code=%d stdout=%q stderr=%q", args, code, out.String(), errOut.String())
		}
	}
}

func TestRunInitUsesPureGoScaffold(t *testing.T) {
	t.Setenv("PATH", fakePrereqBin(t))
	destination := filepath.Join(t.TempDir(), "workspace")
	var out, errOut bytes.Buffer
	if code := runWithInput([]string{"init", destination}, bytes.NewBufferString("n\ny\nn\nn\nn\n"), &out, &errOut); code != exitOK {
		t.Fatalf("code=%d stderr=%s", code, errOut.String())
	}
	if _, err := os.Stat(filepath.Join(destination, ".git")); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltRuntimeWorksWithoutSystemGit(t *testing.T) {
	goBinary, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "smt")
	build := exec.Command(goBinary, "build", "-o", bin, "./cmd/smt")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v: %s", err, output)
	}
	workspace := filepath.Join(t.TempDir(), "workspace")
	init := exec.Command(bin, "init", workspace)
	init.Stdin = strings.NewReader("n\ny\nn\nn\nn\n")
	init.Env = []string{"PATH=" + fakePrereqBin(t)}
	if output, err := init.CombinedOutput(); err != nil {
		t.Fatalf("init: %v: %s", err, output)
	}
	configPath := filepath.Join(workspace, "smt.yaml")
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	config = []byte(strings.ReplaceAll(string(config), "url: \"\"", "url: https://example.invalid/repository.git"))
	if err := os.WriteFile(configPath, config, 0o600); err != nil {
		t.Fatal(err)
	}
	repository, err := ggit.PlainOpen(workspace)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("smt.yaml"); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("configure remotes", &ggit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.invalid", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"status"}, {"push", "--dry-run"}} {
		command := exec.Command(bin, args...)
		command.Dir = workspace
		command.Env = []string{"PATH=" + fakePrereqBin(t)}
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v: %s", args, err, output)
		}
	}
}

func TestRunValidateMessageSuccessUsageAndValidationExit(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := writeMainConfig("smt.yaml", mainConfigYAML()); err != nil {
		t.Fatal(err)
	}
	message := filepath.Join(root, "message")
	if err := os.WriteFile(message, []byte("feat(repo): useful change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"validate-message", message}, exitOK, "valid commit message"},
		{[]string{"validate-message"}, exitUsage, "usage: smt validate-message FILE"},
	} {
		var out, errOut bytes.Buffer
		if got := run(test.args, &out, &errOut); got != test.code || !strings.Contains(out.String()+errOut.String(), test.want) {
			t.Fatalf("run(%v) code=%d stdout=%q stderr=%q", test.args, got, out.String(), errOut.String())
		}
	}
	if err := os.WriteFile(message, []byte("feat: missing scope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if got := run([]string{"validate-message", message}, &out, &errOut); got != exitValidation || !strings.Contains(errOut.String(), "scope is required") {
		t.Fatalf("validation code=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
}

func TestRunVerboseLoggerKeepsArgumentsOutOfDiagnosticsAndHonorsNoColor(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := writeMainConfig("smt.yaml", mainConfigYAML()); err != nil {
		t.Fatal(err)
	}
	message := filepath.Join(root, "private-message")
	if err := os.WriteFile(message, []byte("feat(repo): useful change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NO_COLOR", "1")
	var out, errOut bytes.Buffer
	if got := run([]string{"--verbose", "validate-message", message}, &out, &errOut); got != exitOK {
		t.Fatalf("code=%d stderr=%q", got, errOut.String())
	}
	if out.String() != "valid commit message\n" || !strings.Contains(errOut.String(), "command=validate-message") || strings.Contains(errOut.String(), message) || strings.Contains(errOut.String(), "\x1b[") {
		t.Fatalf("stdout=%q stderr=%q", out.String(), errOut.String())
	}
}

func TestNewRunLoggerColorEnvironment(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("NO_COLOR", "")
	var forced bytes.Buffer
	newRunLogger(true, &forced).Debug("forced colors")
	if !strings.Contains(forced.String(), "\x1b[") {
		t.Fatalf("forced=%q", forced.String())
	}
	t.Setenv("NO_COLOR", "1")
	var disabled bytes.Buffer
	newRunLogger(true, &disabled).Debug("disabled colors")
	if strings.Contains(disabled.String(), "\x1b[") {
		t.Fatalf("disabled=%q", disabled.String())
	}
}

func TestRunNormalModeDoesNotWriteDebugDiagnostics(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	if err := writeMainConfig("smt.yaml", mainConfigYAML()); err != nil {
		t.Fatal(err)
	}
	message := filepath.Join(root, "message")
	if err := os.WriteFile(message, []byte("feat(repo): useful change\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if got := run([]string{"validate-message", message}, &out, &errOut); got != exitOK || errOut.Len() != 0 {
		t.Fatalf("code=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
}

func TestRunStatusHumanAndJSONOutputContinueAcrossRepositories(t *testing.T) {
	root := newMainRepository(t)
	t.Chdir(root)
	if err := writeMainConfig("smt.yaml", `version: 1
commit: {types: [feat], scopes: [repo, missing]}
repositories:
  - {id: repo, path: ., scope: repo}
  - {id: missing, path: missing, scope: missing}
`); err != nil {
		t.Fatal(err)
	}
	commitMainFiles(t, root, "configure")
	var humanOut, errOut bytes.Buffer
	if got := run([]string{"status"}, &humanOut, &errOut); got != exitOK || !strings.Contains(humanOut.String(), "repo: clean") || !strings.Contains(humanOut.String(), "missing: uninitialized") {
		t.Fatalf("code=%d stdout=%q stderr=%q", got, humanOut.String(), errOut.String())
	}
	var jsonOut bytes.Buffer
	if got := run([]string{"status", "--json"}, &jsonOut, &errOut); got != exitOK {
		t.Fatalf("JSON status code=%d stderr=%q", got, errOut.String())
	}
	var report struct {
		Repositories []struct {
			ID string `json:"id"`
		} `json:"repositories"`
	}
	if err := json.Unmarshal(jsonOut.Bytes(), &report); err != nil || len(report.Repositories) != 2 || report.Repositories[1].ID != "missing" {
		t.Fatalf("JSON=%q err=%v", jsonOut.String(), err)
	}
}

func TestRunDoctorDoesNotRequireGitExecutableOrExposeToken(t *testing.T) {
	root := newMainRepository(t)
	t.Chdir(root)
	if err := writeMainConfig("smt.yaml", `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - {id: repo, path: ., provider: gitlab, project: sanovy/repo, scope: repo}
`); err != nil {
		t.Fatal(err)
	}
	commitMainFiles(t, root, "configure")
	secret := "doctor-token-must-not-appear"
	t.Setenv("SMT_GITLAB_TOKEN", secret)
	var out, errOut bytes.Buffer
	if got := run([]string{"doctor"}, &out, &errOut); got != exitOK || strings.Contains(out.String()+errOut.String(), secret) || !strings.Contains(out.String(), "token:gitlab: ok") {
		t.Fatalf("code=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
}

func TestRunCheckMutationGuardAndContractsCommands(t *testing.T) {
	root := newMainRepository(t)
	t.Chdir(root)
	contents := `version: 1
commit: {types: [feat], scopes: [repo]}
repositories:
  - id: repo
    path: .
    scope: repo
    checks:
      hook:
        - {kind: sql-format, argv: [pg_format], include: ["**/*.sql"], mutates_worktree: true}
contracts:
  reference:
    - {id: ci-pin, repository: repo, file: contract.txt, expected: old, replacement: new}
  artifact:
    - {id: missing, repository: repo, file: missing.txt, expected: present}
`
	if err := writeMainConfig("smt.yaml", contents); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("contract.txt", []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitMainFiles(t, root, "configure")
	var out, errOut bytes.Buffer
	if got := run([]string{"check", "--profile", "hook"}, &out, &errOut); got != exitValidation || !strings.Contains(errOut.String(), "--allow-worktree-mutation") {
		t.Fatalf("check code=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if got := run([]string{"contracts", "validate"}, &out, &errOut); got != exitValidation || !strings.Contains(out.String(), "missing") {
		t.Fatalf("contracts code=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if got := run([]string{"ci", "audit"}, &out, &errOut); got != exitValidation || !strings.Contains(out.String(), "missing") {
		t.Fatalf("audit code=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	out.Reset()
	errOut.Reset()
	if got := run([]string{"ci", "contracts", "bump", "--id", "ci-pin"}, &out, &errOut); got != exitOK || !strings.Contains(out.String(), "--- after") {
		t.Fatalf("bump plan code=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	if content, _ := os.ReadFile("contract.txt"); string(content) != "old\n" {
		t.Fatalf("plan changed contract=%q", content)
	}
	out.Reset()
	errOut.Reset()
	if got := run([]string{"ci", "contracts", "bump", "--id", "ci-pin", "--apply"}, &out, &errOut); got != exitOK {
		t.Fatalf("bump apply code=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	if content, _ := os.ReadFile("contract.txt"); string(content) != "new\n" {
		t.Fatalf("apply contract=%q", content)
	}
}

func TestRunPushUsesConfiguredFileRemotesChildFirst(t *testing.T) {
	root := filepath.Join(t.TempDir(), "platform")
	if _, err := scaffold.New(mainReadyPrerequisites{}).Init(context.Background(), root, scaffold.Selection{Web: true, API: true, Codex: true}); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "smt.yaml")
	cfg, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	remoteRoot := t.TempDir()
	for index := range cfg.Repositories {
		remote := filepath.Join(remoteRoot, cfg.Repositories[index].ID+".git")
		if _, err := ggit.PlainInit(remote, true); err != nil {
			t.Fatal(err)
		}
		cfg.Repositories[index].Remote.URL = remote
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	commitMainFiles(t, root, "configure remotes")
	t.Chdir(root)
	var out, errOut bytes.Buffer
	if got := run([]string{"push"}, &out, &errOut); got != exitOK {
		t.Fatalf("push code=%d stdout=%q stderr=%q", got, out.String(), errOut.String())
	}
	positions := []int{strings.Index(out.String(), "pushed web"), strings.Index(out.String(), "pushed api"), strings.Index(out.String(), "pushed repo")}
	if positions[0] < 0 || positions[1] < positions[0] || positions[2] < positions[1] {
		t.Fatalf("push order stdout=%q", out.String())
	}
}

type mainReadyPrerequisites struct{}

func (mainReadyPrerequisites) Check(_ context.Context, requirements prereq.Requirements) (prereq.Result, error) {
	findings := []prereq.Finding{{ID: "codex", Status: prereq.StatusReady}, {ID: "asdf", Status: prereq.StatusReady}, {ID: "bd", Status: prereq.StatusReady}}
	for _, plugin := range requirements.Plugins {
		findings = append(findings, prereq.Finding{ID: "codex-plugin:" + plugin.Selector, Status: prereq.StatusReady})
	}
	for _, runtime := range requirements.Runtimes {
		findings = append(findings, prereq.Finding{ID: "asdf-runtime:" + runtime.Plugin + "@" + runtime.Version, Status: prereq.StatusReady})
	}
	return prereq.Result{Findings: findings}, nil
}

func (mainReadyPrerequisites) InitBeads(_ context.Context, dir, prefix string) error {
	if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0o755); err != nil {
		return err
	}
	for path, contents := range map[string]string{
		".beads/.gitignore":         "embeddeddolt/\n",
		".beads/README.md":          "# Beads\n",
		".beads/config.yaml":        "issue-prefix: " + prefix + "\n",
		".beads/metadata.json":      "{\"dolt_database\":\"" + prefix + "\"}\n",
		".beads/interactions.jsonl": "",
	} {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(contents), 0o600); err != nil {
			return err
		}
	}
	return nil
}

func fakePrereqBin(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"codex": "#!/bin/sh\nprintf '%s\\n' '{\"installed\":[{\"pluginId\":\"codex-obsidian@codex-obsidian\",\"installed\":true,\"enabled\":true,\"marketplaceSource\":{\"source\":\"parmcoder/codex-obsidian\"}},{\"pluginId\":\"godex@godex\",\"installed\":true,\"enabled\":true,\"marketplaceSource\":{\"source\":\"parmcoder/godex\"}}],\"available\":[]}'\n",
		"asdf":  "#!/bin/sh\nif [ \"$1\" = plugin ]; then printf '%s\\n' 'task' 'lefthook' 'golang' 'nodejs' 'opentofu'; else printf '%s\\n' '3.52.0' '2.1.10' '1.26.5' '24.18.0' '1.12.3'; fi\n",
		"bd":    "#!/bin/sh\n/bin/mkdir -p .beads\nprintf '%s\\n' 'embeddeddolt/' > .beads/.gitignore\nprintf '%s\\n' '# Beads' > .beads/README.md\nprintf '%s\\n' 'issue-prefix: test' > .beads/config.yaml\nprintf '%s\\n' '{\"prefix\":\"test\"}' > .beads/metadata.json\n: > .beads/interactions.jsonl\n",
	}
	for name, contents := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func mainConfigYAML() string {
	return "version: 1\ncommit:\n  types: [feat]\n  scopes: [repo]\nrepositories:\n  - id: repo\n    path: .\n    scope: repo\n"
}

func writeMainConfig(path, contents string) error { return os.WriteFile(path, []byte(contents), 0o600) }

func newMainRepository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	_, err := ggit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("initial\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commitMainFiles(t, dir, "initial")
	return dir
}

func commitMainFiles(t *testing.T, dir, message string) {
	t.Helper()
	repository, err := ggit.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := worktree.AddWithOptions(&ggit.AddOptions{All: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit(message, &ggit.CommitOptions{Author: &object.Signature{Name: "SMT Test", Email: "smt@example.invalid", When: time.Now()}}); err != nil {
		t.Fatal(err)
	}
}
