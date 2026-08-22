package apply

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

func TestMobileE2EApplyEmitsRunnerAndPreservesNativeContract(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyMobileE2EWorkspace(t, destination)

	entries, err := os.ReadDir(filepath.Join(destination, "e2e", "mobile"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	wantNames := []string{".env.example", ".gitignore", "README.md", "run.sh"}
	if fmt.Sprint(names) != fmt.Sprint(wantNames) {
		t.Fatalf("e2e/mobile files=%v, want %v", names, wantNames)
	}

	integration, err := os.ReadFile(filepath.Join(destination, "mobile-app", "integration_test", "app_test.dart"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"IntegrationTestWidgetsFlutterBinding", "mobile-home", "api-status"} {
		if !strings.Contains(string(integration), marker) {
			t.Fatalf("native integration contract missing %q:\n%s", marker, integration)
		}
	}
}

func TestMobileE2EApplyEmitsNoRunnerWithoutBothDeclarationAndMobile(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		fake func(*testing.T)
	}{
		{name: "mobile without e2e", raw: mobileBlueprintBytes(), fake: func(t *testing.T) { installFakeASDF(t, false) }},
		{name: "e2e without mobile", raw: e2eWebBlueprintBytes(), fake: func(t *testing.T) { installFakeNextASDF(t, false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fake(t)
			destination := filepath.Join(t.TempDir(), "workspace")
			applyWorkspace(t, destination, tt.raw)
			if _, err := os.Stat(filepath.Join(destination, "e2e", "mobile")); !os.IsNotExist(err) {
				t.Fatalf("e2e/mobile exists: %v", err)
			}
		})
	}
}

func TestMobileE2EApplyIsDeterministic(t *testing.T) {
	installFakeASDF(t, false)
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	applyMobileE2EWorkspace(t, first)
	applyMobileE2EWorkspace(t, second)

	for _, relative := range []string{".env.example", ".gitignore", "README.md", "run.sh"} {
		left, err := os.ReadFile(filepath.Join(first, "e2e", "mobile", relative))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(second, "e2e", "mobile", relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(left) != string(right) {
			t.Fatalf("generated e2e/mobile/%s differs across fresh destinations", relative)
		}
	}
}

func TestMobileE2EGeneratedContractIsCredentialFreeAndExplicit(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyMobileE2EWorkspace(t, destination)

	for _, relative := range []string{".env.example", ".gitignore", "README.md", "run.sh"} {
		contents, err := os.ReadFile(filepath.Join(destination, "e2e", "mobile", relative))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(contents))
		for _, forbidden := range []string{"credential", "password", "secret", "signing", "device farm", "package install"} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("generated %s contains forbidden marker %q:\n%s", relative, forbidden, contents)
			}
		}
	}

	runner, err := os.ReadFile(filepath.Join(destination, "e2e", "mobile", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"--platform android|ios",
		"--device",
		"asdf exec flutter test integration_test/app_test.dart",
		"--dart-define=SMT_API_BASE_URL=",
		"status=unavailable",
		"reports/",
	} {
		if !strings.Contains(string(runner), required) {
			t.Fatalf("runner missing %q:\n%s", required, runner)
		}
	}
}

func TestMobileE2ERunnerRequiresExplicitPlatformAndDevice(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyMobileE2EWorkspace(t, destination)
	runner := filepath.Join(destination, "e2e", "mobile", "run.sh")

	for _, args := range [][]string{{"--device", "emulator-5554"}, {"--platform", "android"}} {
		cmd := exec.Command("sh", append([]string{runner}, args...)...)
		if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "required") {
			t.Fatalf("runner args=%v output=%q err=%v, want explicit-argument failure", args, output, err)
		}
	}
}

func TestMobileE2ERunnerPassesDeviceAndAPIDefine(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyMobileE2EWorkspace(t, destination)
	logPath := installFakeMobileE2EASDF(t, "emulator-5554 • android\n", 0, 0)
	t.Setenv("SMT_API_BASE_URL", "http://127.0.0.1:8080")

	runGeneratedMobileE2E(t, destination, "android", "emulator-5554")
	status := readMobileE2EReport(t, destination, "android.status")
	if !strings.Contains(status, "status=passed") {
		t.Fatalf("status=%q, want passed", status)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"arg=exec",
		"arg=flutter",
		"arg=test",
		"arg=integration_test/app_test.dart",
		"arg=-d",
		"arg=emulator-5554",
		"arg=--dart-define=SMT_API_BASE_URL=http://127.0.0.1:8080",
	} {
		if !strings.Contains(string(log), marker) {
			t.Fatalf("fake Flutter log missing %q:\n%s", marker, log)
		}
	}
}

func TestMobileE2ERunnerReportsUnavailableDevice(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyMobileE2EWorkspace(t, destination)
	installFakeMobileE2EASDF(t, "no-target (mobile)\n", 0, 0)

	output, err := runGeneratedMobileE2EOutput(t, destination, "ios", "iphone-unknown")
	if err == nil || !strings.Contains(string(output), "unavailable") {
		t.Fatalf("output=%q err=%v, want unavailable failure", output, err)
	}
	status := readMobileE2EReport(t, destination, "ios.status")
	if !strings.Contains(status, "status=unavailable") {
		t.Fatalf("status=%q, want unavailable", status)
	}
	log := readMobileE2EReport(t, destination, "ios.log")
	if !strings.Contains(log, "no-target") {
		t.Fatalf("log=%q, want device listing preserved", log)
	}
}

func TestMobileE2ERunnerReportsFailedTest(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyMobileE2EWorkspace(t, destination)
	installFakeMobileE2EASDF(t, "emulator-5554 (mobile)\n", 0, 17)

	output, err := runGeneratedMobileE2EOutput(t, destination, "android", "emulator-5554")
	if err == nil || !strings.Contains(string(output), "failed") {
		t.Fatalf("output=%q err=%v, want failed-test error", output, err)
	}
	status := readMobileE2EReport(t, destination, "android.status")
	if !strings.Contains(status, "status=failed") {
		t.Fatalf("status=%q, want failed", status)
	}
	log := readMobileE2EReport(t, destination, "android.log")
	if !strings.Contains(log, "FAKE_FLUTTER_TEST_OUTPUT") {
		t.Fatalf("log=%q, want test output preserved", log)
	}
}

func applyMobileE2EWorkspace(t *testing.T, destination string) {
	t.Helper()
	applyWorkspace(t, destination, mobileE2EBlueprintBytes())
}

func applyWorkspace(t *testing.T, destination string, raw []byte) {
	t.Helper()
	cfg, err := config.LoadBytes(raw, filepath.Join(t.TempDir(), "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	if err := service.Apply(context.Background(), destination, raw); err != nil {
		t.Fatal(err)
	}
}

func installFakeMobileE2EASDF(t *testing.T, devices string, devicesStatus, testStatus int) string {
	t.Helper()
	directory := t.TempDir()
	logPath := filepath.Join(directory, "asdf.log")
	script := "#!/bin/sh\n" +
		"set -eu\n" +
		"log=\"$SMT_FAKE_MOBILE_E2E_LOG\"\n" +
		"printf 'cwd=%s\\n' \"$PWD\" >> \"$log\"\n" +
		"for arg in \"$@\"; do printf 'arg=%s\\n' \"$arg\" >> \"$log\"; done\n" +
		"case \"${3:-}\" in\n" +
		"devices)\n" +
		"  printf '%s' \"$SMT_FAKE_MOBILE_DEVICES\"\n" +
		"  exit \"${SMT_FAKE_MOBILE_DEVICES_STATUS:-0}\"\n" +
		"  ;;\n" +
		"test)\n" +
		"  printf 'FAKE_FLUTTER_TEST_OUTPUT\\n'\n" +
		"  exit \"${SMT_FAKE_MOBILE_TEST_STATUS:-0}\"\n" +
		"  ;;\n" +
		"esac\n" +
		"exit 127\n"
	if err := os.WriteFile(filepath.Join(directory, "asdf"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("SMT_FAKE_MOBILE_E2E_LOG", logPath)
	t.Setenv("SMT_FAKE_MOBILE_DEVICES", devices)
	t.Setenv("SMT_FAKE_MOBILE_DEVICES_STATUS", fmt.Sprint(devicesStatus))
	t.Setenv("SMT_FAKE_MOBILE_TEST_STATUS", fmt.Sprint(testStatus))
	return logPath
}

func runGeneratedMobileE2E(t *testing.T, destination, platform, device string) {
	t.Helper()
	if output, err := runGeneratedMobileE2EOutput(t, destination, platform, device); err != nil {
		t.Fatalf("runner output=%q err=%v", output, err)
	}
}

func runGeneratedMobileE2EOutput(t *testing.T, destination, platform, device string) ([]byte, error) {
	t.Helper()
	runner := filepath.Join(destination, "e2e", "mobile", "run.sh")
	cmd := exec.Command("sh", runner, "--platform", platform, "--device", device)
	return cmd.CombinedOutput()
}

func readMobileE2EReport(t *testing.T, destination, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(destination, "e2e", "mobile", "reports", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func mobileE2EBlueprintBytes() []byte {
	return []byte("version: 1\n" +
		"provenance: {tool: smt, smt_version: v0.1.0, template_set_version: v1}\n" +
		"workspace: {ai_assist: codex, stack: {mobile: flutter}}\n" +
		"commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, mobile]}\n" +
		"repositories:\n" +
		"  - {id: repo, path: ., scope: repo, modules: [e2e], remote: {url: \"\"}}\n" +
		"  - {id: mobile, path: mobile-app, component: mobile, technology: flutter, scope: mobile, modules: [mobile], remote: {url: \"\"}}\n" +
		"workflow:\n" +
		"  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}\n" +
		"  plugins:\n" +
		"    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}\n" +
		"    - {source: parmcoder/godex, selectors: [godex-go-backend]}\n")
}

func e2eWebBlueprintBytes() []byte {
	return []byte("version: 1\n" +
		"provenance: {tool: smt, smt_version: v0.1.0, template_set_version: v1}\n" +
		"workspace: {ai_assist: codex, stack: {web: nextjs}}\n" +
		"commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, web]}\n" +
		"repositories:\n" +
		"  - {id: repo, path: ., scope: repo, modules: [e2e], remote: {url: \"\"}}\n" +
		"  - {id: web, path: web-app, component: web, technology: nextjs, scope: web, modules: [web], remote: {url: \"\"}}\n" +
		"workflow:\n" +
		"  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}\n" +
		"  plugins:\n" +
		"    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}\n" +
		"    - {source: parmcoder/godex, selectors: [godex-go-backend]}\n")
}
