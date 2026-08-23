package apply

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestE2EOrchestrationApplyEmitsCoordinatorForSelectedTargets(t *testing.T) {
	tests := []struct {
		name       string
		raw        []byte
		install    func(*testing.T)
		wantWeb    bool
		wantMobile bool
	}{
		{name: "web", raw: e2eWebBlueprintBytes(), install: func(t *testing.T) { installFakeNextASDF(t, false) }, wantWeb: true},
		{name: "mobile", raw: mobileE2EBlueprintBytes(), install: func(t *testing.T) { installFakeASDF(t, false) }, wantMobile: true},
		{name: "web and mobile", raw: fullE2EBlueprintBytes(), install: func(t *testing.T) { installFakeASDF(t, false) }, wantWeb: true, wantMobile: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.install(t)
			destination := filepath.Join(t.TempDir(), "workspace")
			applyWorkspace(t, destination, tt.raw)
			assertE2EOrchestrationFiles(t, destination)
			assertE2EChildSelection(t, destination, tt.wantWeb, tt.wantMobile)
			assertE2ERootEntries(t, destination, tt.wantWeb, tt.wantMobile)
			if _, err := os.Stat(filepath.Join(destination, "e2e", "reports")); !os.IsNotExist(err) {
				t.Fatalf("Apply created aggregate reports directory: %v", err)
			}
		})
	}
}

func TestE2EOrchestrationApplyPreservesMetadataOnlyAndNonE2EBehavior(t *testing.T) {
	tests := []struct {
		name    string
		raw     []byte
		install func(*testing.T)
	}{
		{name: "e2e without targets", raw: e2eOnlyBlueprintBytes(), install: func(*testing.T) {}},
		{name: "without e2e", raw: blueprintBytes(), install: func(t *testing.T) { installFakeNextASDF(t, false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.install(t)
			destination := filepath.Join(t.TempDir(), "workspace")
			applyWorkspace(t, destination, tt.raw)
			for _, relative := range coordinatorSupportFiles() {
				if _, err := os.Stat(filepath.Join(destination, relative)); !os.IsNotExist(err) {
					t.Fatalf("unexpected coordinator artifact %s: %v", relative, err)
				}
			}
			if tt.name == "without e2e" {
				if _, err := os.Stat(filepath.Join(destination, "Taskfile.yml")); err != nil {
					t.Fatalf("OCI workspace is missing its root Compose Taskfile: %v", err)
				}
			} else if _, err := os.Stat(filepath.Join(destination, "Taskfile.yml")); !os.IsNotExist(err) {
				t.Fatalf("metadata-only e2e workspace unexpectedly has a root Taskfile: %v", err)
			}
		})
	}
}

func TestE2EOrchestrationOutputIsDeterministic(t *testing.T) {
	installFakeNextASDF(t, false)
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	applyWorkspace(t, first, e2eWebBlueprintBytes())
	applyWorkspace(t, second, e2eWebBlueprintBytes())
	for _, relative := range coordinatorGeneratedFiles() {
		left, err := os.ReadFile(filepath.Join(first, relative))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(second, relative))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(left, right) {
			t.Fatalf("generated %s differs across fresh Apply destinations", relative)
		}
	}
}

func TestE2EOrchestrationGeneratedContractIsExplicitAndCredentialFree(t *testing.T) {
	installFakeNextASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, e2eWebBlueprintBytes())
	var combined strings.Builder
	for _, relative := range coordinatorGeneratedFiles() {
		contents, err := os.ReadFile(filepath.Join(destination, relative))
		if err != nil {
			t.Fatal(err)
		}
		combined.Write(contents)
	}
	text := strings.ToLower(combined.String())
	for _, forbidden := range []string{"password", "authorization", "credential", "signing", "cloud device farm", "remote ci", "crud", "domain fixture", "process manager", "device manager", "npm install", "flutter pub get", "playwright install"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("coordinator output contains forbidden marker %q", forbidden)
		}
	}
	taskfile, err := os.ReadFile(filepath.Join(destination, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"web:", "mobile:", "verify:", "e2e/web/run.sh", "e2e/mobile/run.sh", "PLATFORM", "DEVICE", "SMT_API_BASE_URL", "status=unavailable", "status=failed", "status=passed"} {
		if !strings.Contains(string(taskfile), required) {
			t.Fatalf("Taskfile missing %q:\n%s", required, taskfile)
		}
	}
}

func TestE2EOrchestrationTaskForwardsLaneVariables(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, fullE2EBlueprintBytes())
	logPath := filepath.Join(t.TempDir(), "coordinator.log")
	writeFakeE2ERunner(t, filepath.Join(destination, "e2e", "web", "run.sh"), "web", 0, "WEB_RUNNER_OUTPUT")
	writeFakeE2ERunner(t, filepath.Join(destination, "e2e", "mobile", "run.sh"), "mobile", 0, "MOBILE_RUNNER_OUTPUT")
	t.Setenv("FAKE_E2E_LOG", logPath)

	if output, err := runGeneratedE2ETask(t, destination, "BROWSER=firefox", "web"); err != nil {
		t.Fatalf("web task output=%q err=%v", output, err)
	}
	if output, err := runGeneratedE2ETask(t, destination, "PLATFORM=android", "DEVICE=emulator-1", "API_BASE_URL=http://127.0.0.1:8080", "mobile"); err != nil {
		t.Fatalf("mobile task output=%q err=%v", output, err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(log)
	for _, marker := range []string{"lane=web", "arg=--browser", "arg=firefox", "lane=mobile", "arg=--platform", "arg=android", "arg=--device", "arg=emulator-1", "api=http://127.0.0.1:8080"} {
		if !strings.Contains(text, marker) {
			t.Fatalf("coordinator forwarding log missing %q:\n%s", marker, text)
		}
	}
}

func TestE2EOrchestrationManualUnselectedLanesAreUnavailable(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		task string
	}{
		{name: "mobile from web workspace", raw: e2eWebBlueprintBytes(), task: "mobile"},
		{name: "web from mobile workspace", raw: mobileE2EBlueprintBytes(), task: "web"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.task == "web" {
				installFakeASDF(t, false)
			} else {
				installFakeNextASDF(t, false)
			}
			destination := filepath.Join(t.TempDir(), "workspace")
			applyWorkspace(t, destination, tt.raw)
			output, err := runGeneratedE2ETask(t, destination, tt.task)
			if err == nil || !strings.Contains(strings.ToLower(string(output)), "status=unavailable") {
				t.Fatalf("task %s output=%q err=%v, want actionable unavailable result", tt.task, output, err)
			}
		})
	}
}

func TestE2EOrchestrationVerifyRunsOnlySelectedTargets(t *testing.T) {
	installFakeNextASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, e2eWebBlueprintBytes())
	logPath := filepath.Join(t.TempDir(), "coordinator.log")
	writeFakeE2ERunner(t, filepath.Join(destination, "e2e", "web", "run.sh"), "web", 0, "WEB_RUNNER_OUTPUT")
	t.Setenv("FAKE_E2E_LOG", logPath)
	if output, err := runGeneratedE2ETask(t, destination, "verify"); err != nil {
		t.Fatalf("verify output=%q err=%v", output, err)
	}
	status, err := os.ReadFile(filepath.Join(destination, "e2e", "reports", "verify.status"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(status), "status=passed") {
		t.Fatalf("verify status=%q, want passed", status)
	}
	invocations, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(invocations), "lane=mobile") || !strings.Contains(string(invocations), "lane=web") {
		t.Fatalf("verify selected-lane invocations=%q", invocations)
	}
}

func TestE2EOrchestrationMobileTaskRequiresExplicitInputs(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, mobileE2EBlueprintBytes())
	for _, args := range [][]string{{"mobile"}, {"PLATFORM=android", "mobile"}, {"DEVICE=emulator-1", "mobile"}} {
		output, err := runGeneratedE2ETask(t, destination, args...)
		if err == nil || !strings.Contains(strings.ToLower(string(output)), "platform") || !strings.Contains(strings.ToLower(string(output)), "device") {
			t.Fatalf("task args=%v output=%q err=%v, want explicit platform/device failure", args, output, err)
		}
	}
}

func TestE2EOrchestrationVerifyAggregatesAllLaneResults(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, fullE2EBlueprintBytes())
	tests := []struct {
		name         string
		webStatus    int
		mobileStatus int
		wantStatus   string
		wantError    bool
	}{
		{name: "passed", webStatus: 0, mobileStatus: 0, wantStatus: "passed"},
		{name: "unavailable", webStatus: 0, mobileStatus: 3, wantStatus: "unavailable", wantError: true},
		{name: "failed takes precedence", webStatus: 17, mobileStatus: 3, wantStatus: "failed", wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logPath := filepath.Join(t.TempDir(), "coordinator.log")
			t.Setenv("FAKE_E2E_LOG", logPath)
			writeFakeE2ERunner(t, filepath.Join(destination, "e2e", "web", "run.sh"), "web", tt.webStatus, "WEB_RUNNER_OUTPUT")
			writeFakeE2ERunner(t, filepath.Join(destination, "e2e", "mobile", "run.sh"), "mobile", tt.mobileStatus, "MOBILE_RUNNER_OUTPUT")
			output, err := runGeneratedE2ETask(t, destination, "BROWSER=chromium", "PLATFORM=android", "DEVICE=emulator-1", "API_BASE_URL=http://127.0.0.1:8080", "verify")
			if (err != nil) != tt.wantError {
				t.Fatalf("verify output=%q err=%v, wantError=%v", output, err, tt.wantError)
			}
			status, readErr := os.ReadFile(filepath.Join(destination, "e2e", "reports", "verify.status"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !strings.Contains(string(status), "status="+tt.wantStatus) {
				t.Fatalf("verify status=%q, want %q", status, tt.wantStatus)
			}
			log, readErr := os.ReadFile(filepath.Join(destination, "e2e", "reports", "verify.log"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, marker := range []string{"WEB_RUNNER_OUTPUT", "MOBILE_RUNNER_OUTPUT"} {
				if !strings.Contains(string(log), marker) {
					t.Fatalf("verify log missing %q:\n%s", marker, log)
				}
			}
			invocations, readErr := os.ReadFile(logPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			for _, marker := range []string{"lane=web", "lane=mobile"} {
				if !strings.Contains(string(invocations), marker) {
					t.Fatalf("verify did not invoke %q:\n%s", marker, invocations)
				}
			}
		})
	}
}

func assertE2EOrchestrationFiles(t *testing.T, destination string) {
	t.Helper()
	for _, relative := range coordinatorGeneratedFiles() {
		if _, err := os.Stat(filepath.Join(destination, relative)); err != nil {
			t.Fatalf("missing coordinator artifact %s: %v", relative, err)
		}
	}
	readme, err := os.ReadFile(filepath.Join(destination, "e2e", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"task web", "task mobile", "task verify", "PLATFORM", "DEVICE", "e2e/reports/"} {
		if !strings.Contains(string(readme), marker) {
			t.Fatalf("e2e README missing %q:\n%s", marker, readme)
		}
	}
	ignore, err := os.ReadFile(filepath.Join(destination, "e2e", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "reports/") {
		t.Fatalf("e2e .gitignore does not ignore reports: %q", ignore)
	}
}

func assertE2EChildSelection(t *testing.T, destination string, wantWeb, wantMobile bool) {
	t.Helper()
	webErr := assertPathExists(filepath.Join(destination, "e2e", "web"))
	mobileErr := assertPathExists(filepath.Join(destination, "e2e", "mobile"))
	if (webErr == nil) != wantWeb {
		t.Fatalf("e2e/web selected=%v, want %v, err=%v", webErr == nil, wantWeb, webErr)
	}
	if (mobileErr == nil) != wantMobile {
		t.Fatalf("e2e/mobile selected=%v, want %v, err=%v", mobileErr == nil, wantMobile, mobileErr)
	}
}

func assertE2ERootEntries(t *testing.T, destination string, wantWeb, wantMobile bool) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(destination, "e2e"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "README.md"}
	if wantMobile {
		want = append(want, "mobile")
	}
	if wantWeb {
		want = append(want, "web")
	}
	var got []string
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("generated e2e entries=%v, want exactly %v", got, want)
	}
}

func assertPathExists(path string) error {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return err
	}
	return nil
}

func coordinatorGeneratedFiles() []string {
	return []string{"Taskfile.yml", filepath.Join("e2e", "README.md"), filepath.Join("e2e", ".gitignore")}
}

func coordinatorSupportFiles() []string {
	return []string{filepath.Join("e2e", "README.md"), filepath.Join("e2e", ".gitignore")}
}

func fullE2EBlueprintBytes() []byte {
	raw := string(mobileBlueprintBytes())
	raw = strings.Replace(raw, "scope: repo, remote:", "scope: repo, modules: [e2e], remote:", 1)
	return []byte(raw)
}

func e2eOnlyBlueprintBytes() []byte {
	return []byte("version: 1\n" +
		"provenance: {tool: smt, smt_version: v0.1.0, template_set_version: v1}\n" +
		"commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo]}\n" +
		"repositories:\n" +
		"  - {id: repo, path: ., scope: repo, modules: [e2e], remote: {url: \"\"}}\n" +
		"workflow:\n" +
		"  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}\n" +
		"  plugins:\n" +
		"    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}\n" +
		"    - {source: parmcoder/godex, selectors: [godex-go-backend]}\n")
}

func writeFakeE2ERunner(t *testing.T, path, lane string, status int, output string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
set -eu
log="$FAKE_E2E_LOG"
echo "lane=%s" >> "$log"
for arg in "$@"; do
	echo "arg=$arg" >> "$log"
done
echo "api=$SMT_API_BASE_URL" >> "$log"
echo %q
exit %d
`, lane, output, status)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func runGeneratedE2ETask(t *testing.T, destination string, args ...string) ([]byte, error) {
	t.Helper()
	taskPath := generatedE2ETaskBinary(t)
	commandArgs := make([]string, 0, len(args))
	environment := append(os.Environ(), "SMT_API_BASE_URL=")
	for _, arg := range args {
		if strings.Contains(arg, "=") {
			environment = append(environment, arg)
			continue
		}
		commandArgs = append(commandArgs, arg)
	}
	command := exec.Command(taskPath, commandArgs...)
	command.Dir = destination
	command.Env = environment
	return command.CombinedOutput()
}

func generatedE2ETaskBinary(t *testing.T) string {
	t.Helper()
	taskPath, err := exec.LookPath("task")
	if err != nil {
		t.Fatalf("task executable is required for orchestration tests: %v", err)
	}
	shim, err := os.ReadFile(taskPath)
	if err != nil {
		t.Fatalf("read task executable %s: %v", taskPath, err)
	}
	if !strings.Contains(string(shim), "asdf exec") {
		return taskPath
	}
	asdfRoot := filepath.Dir(filepath.Dir(taskPath))
	matches, err := filepath.Glob(filepath.Join(asdfRoot, "installs", "task", "*", "bin", "task"))
	if err != nil || len(matches) == 0 {
		t.Fatalf("resolve Task binary behind asdf shim %s: %v", taskPath, err)
	}
	return matches[len(matches)-1]
}
