package apply

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWebE2EApplyEmitsContractPackageAndPreservesWebRuntime(t *testing.T) {
	installFakeNextASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, e2eWebBlueprintBytes())

	assertWebE2EFileSet(t, destination)
	if _, err := os.Stat(filepath.Join(destination, "e2e", "mobile")); !os.IsNotExist(err) {
		t.Fatalf("Web-only Apply emitted e2e/mobile: %v", err)
	}
	for relative, markers := range map[string][]string{
		filepath.Join("web-app", "app", "page.tsx"):            {`data-smt-web-smoke="home"`},
		filepath.Join("web-app", "app", "healthz", "route.ts"): {`status: "ok"`},
	} {
		contents, err := os.ReadFile(filepath.Join(destination, relative))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range markers {
			if !strings.Contains(string(contents), marker) {
				t.Errorf("Web runtime %s missing %q", relative, marker)
			}
		}
	}
}

func TestWebE2EGeneratedAssetsUsePnpmGuidance(t *testing.T) {
	installFakeNextASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, e2eWebBlueprintBytes())

	for relative, markers := range map[string][]string{
		"README.md": {
			"asdf exec pnpm install",
			"asdf exec pnpm run build",
			"asdf exec pnpm exec playwright install chromium",
		},
		"playwright.config.ts": {"asdf exec pnpm run start"},
		"run.sh": {
			"pnpm-lock.yaml",
			"asdf exec pnpm --version",
			"asdf exec pnpm run test",
			"pnpm exec playwright install",
		},
	} {
		contents, err := os.ReadFile(filepath.Join(destination, "e2e", "web", relative))
		if err != nil {
			t.Fatal(err)
		}
		text := string(contents)
		for _, marker := range markers {
			if !strings.Contains(text, marker) {
				t.Errorf("generated e2e/web/%s missing %q:\n%s", relative, marker, text)
			}
		}
		if containsStandaloneNpmCommand(text) || strings.Contains(text, "package-lock.json") {
			t.Errorf("generated e2e/web/%s contains stale npm lockfile guidance:\n%s", relative, text)
		}
	}
}

func TestWebE2EApplyEmitsNoPackageWithoutMatchingWebAndDeclaration(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		fake func(*testing.T)
	}{
		{name: "web without e2e", raw: blueprintBytes(), fake: func(t *testing.T) { installFakeNextASDF(t, false) }},
		{name: "e2e without web", raw: mobileE2EBlueprintBytes(), fake: func(t *testing.T) { installFakeASDF(t, false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fake(t)
			destination := filepath.Join(t.TempDir(), "workspace")
			applyWorkspace(t, destination, tt.raw)
			if _, err := os.Stat(filepath.Join(destination, "e2e", "web")); !os.IsNotExist(err) {
				t.Fatalf("e2e/web exists: %v", err)
			}
		})
	}
}

func TestWebE2EApplyIsDeterministicAcrossFreshDestinations(t *testing.T) {
	installFakeNextASDF(t, false)
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	applyWorkspace(t, first, e2eWebBlueprintBytes())
	applyWorkspace(t, second, e2eWebBlueprintBytes())

	for _, relative := range webE2EGeneratedFiles() {
		left, err := os.ReadFile(filepath.Join(first, "e2e", "web", relative))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(second, "e2e", "web", relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(left) != string(right) {
			t.Fatalf("generated e2e/web/%s differs across fresh destinations", relative)
		}
	}
}

func TestWebE2EGeneratedPackageIsCredentialFreeAndContractOnly(t *testing.T) {
	installFakeNextASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, e2eWebBlueprintBytes())

	for _, relative := range webE2EGeneratedFiles() {
		contents, err := os.ReadFile(filepath.Join(destination, "e2e", "web", relative))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(contents))
		for _, forbidden := range []string{
			"password",
			"authorization",
			"credential",
			"signing",
			"device farm",
			"cloud integration",
			"crud",
			"domain fixture",
		} {
			if strings.Contains(text, forbidden) {
				t.Fatalf("generated e2e/web/%s contains forbidden marker %q", relative, forbidden)
			}
		}
	}

	manifest, err := os.ReadFile(filepath.Join(destination, "e2e", "web", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packageJSON struct {
		Scripts         map[string]string `json:"scripts"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(manifest, &packageJSON); err != nil {
		t.Fatal(err)
	}
	if packageJSON.Scripts["test"] != "playwright test" {
		t.Fatalf("test script=%q, want playwright test", packageJSON.Scripts["test"])
	}
	for name, want := range map[string]string{
		"test:chromium": "SMT_E2E_BROWSER=chromium playwright test --project=chromium",
		"test:firefox":  "SMT_E2E_BROWSER=firefox playwright test --project=firefox",
		"test:webkit":   "SMT_E2E_BROWSER=webkit playwright test --project=webkit",
	} {
		if packageJSON.Scripts[name] != want {
			t.Fatalf("%s script=%q, want %q", name, packageJSON.Scripts[name], want)
		}
	}
	if packageJSON.DevDependencies["@playwright/test"] != "1.62.1" {
		t.Fatalf("Playwright pin=%q, want 1.62.1", packageJSON.DevDependencies["@playwright/test"])
	}

	for relative, markers := range map[string][]string{
		"playwright.config.ts": {
			"http://127.0.0.1:3000",
			"../../web-app",
			"pnpm run start",
			`trace: "on-first-retry"`,
			"Desktop Chrome",
			"Desktop Firefox",
			"Desktop Safari",
			"reports",
		},
		filepath.Join("tests", "contract.smoke.spec.ts"): {
			`data-smt-web-smoke="home"`,
			"toBeVisible",
			"/healthz",
			"toBeOK",
		},
		"run.sh": {
			"--browser",
			"asdf exec pnpm run test",
			"status=unavailable",
			"status=failed",
			"reports/",
			"playwright install",
		},
		"README.md": {
			"asdf exec pnpm install",
			"asdf exec pnpm exec playwright install chromium",
			"/healthz",
			"data-smt-web-smoke=\"home\"",
			"reports/",
		},
	} {
		contents, err := os.ReadFile(filepath.Join(destination, "e2e", "web", relative))
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range markers {
			if !strings.Contains(string(contents), marker) {
				t.Errorf("generated e2e/web/%s missing %q", relative, marker)
			}
		}
	}
}

func TestWebE2ERunnerUsesSelectedBrowserAndWritesPassedReport(t *testing.T) {
	installFakeNextASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, e2eWebBlueprintBytes())
	prepareFakeWebE2EPrerequisites(t, destination, false)
	logPath := installFakeWebE2EASDF(t, 0, false)

	if output, err := runGeneratedWebE2E(t, destination, "--browser", "firefox"); err != nil {
		t.Fatalf("runner output=%q err=%v", output, err)
	}
	status := readWebE2EReport(t, destination, "firefox.status")
	if !strings.Contains(status, "status=passed") {
		t.Fatalf("status=%q, want passed", status)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"arg=exec",
		"arg=node",
		"arg=pnpm",
		"arg=run",
		"arg=test",
		"arg=--project=firefox",
	} {
		if !strings.Contains(string(log), marker) {
			t.Fatalf("fake Web E2E log missing %q:\n%s", marker, log)
		}
	}
}

func TestWebE2ERunnerReportsUnavailablePrerequisites(t *testing.T) {
	installFakeNextASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, e2eWebBlueprintBytes())
	prepareFakeWebE2EPrerequisites(t, destination, true)
	installFakeWebE2EASDF(t, 0, true)

	output, err := runGeneratedWebE2E(t, destination, "--browser", "chromium")
	if err == nil || !strings.Contains(string(output), "unavailable") {
		t.Fatalf("output=%q err=%v, want unavailable failure", output, err)
	}
	status := readWebE2EReport(t, destination, "chromium.status")
	if !strings.Contains(status, "status=unavailable") {
		t.Fatalf("status=%q, want unavailable", status)
	}
	log := readWebE2EReport(t, destination, "chromium.log")
	if !strings.Contains(log, "browser") {
		t.Fatalf("log=%q, want browser prerequisite guidance", log)
	}
}

func TestWebE2ERunnerPreservesFailedTestOutput(t *testing.T) {
	installFakeNextASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, e2eWebBlueprintBytes())
	prepareFakeWebE2EPrerequisites(t, destination, false)
	installFakeWebE2EASDF(t, 17, false)

	output, err := runGeneratedWebE2E(t, destination)
	if err == nil || !strings.Contains(string(output), "failed") {
		t.Fatalf("output=%q err=%v, want failed-test error", output, err)
	}
	status := readWebE2EReport(t, destination, "chromium.status")
	if !strings.Contains(status, "status=failed") {
		t.Fatalf("status=%q, want failed", status)
	}
	log := readWebE2EReport(t, destination, "chromium.log")
	if !strings.Contains(log, "FAKE_PLAYWRIGHT_OUTPUT") {
		t.Fatalf("log=%q, want test output preserved", log)
	}
}

func TestWebE2ERunnerReportsMissingASDF(t *testing.T) {
	installFakeNextASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, e2eWebBlueprintBytes())
	t.Setenv("PATH", "/usr/bin:/bin")

	output, err := runGeneratedWebE2E(t, destination)
	if err == nil || !strings.Contains(string(output), "unavailable") {
		t.Fatalf("output=%q err=%v, want missing-asdf unavailable failure", output, err)
	}
	status := readWebE2EReport(t, destination, "chromium.status")
	if !strings.Contains(status, "status=unavailable") {
		t.Fatalf("status=%q, want unavailable", status)
	}
}

func assertWebE2EFileSet(t *testing.T, destination string) {
	t.Helper()
	root := filepath.Join(destination, "e2e", "web")
	actual := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		actual[filepath.ToSlash(relative)] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for _, relative := range webE2EGeneratedFiles() {
		want[filepath.ToSlash(relative)] = true
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("e2e/web files=%v, want=%v", actual, want)
	}
}

func webE2EGeneratedFiles() []string {
	return []string{
		".env.example",
		".gitignore",
		"README.md",
		"package.json",
		"playwright.config.ts",
		"run.sh",
		filepath.Join("tests", "contract.smoke.spec.ts"),
	}
}

func prepareFakeWebE2EPrerequisites(t *testing.T, destination string, missingBrowser bool) {
	t.Helper()
	web := filepath.Join(destination, "web-app")
	e2e := filepath.Join(destination, "e2e", "web")
	for _, directory := range []string{
		filepath.Join(web, ".next"),
		filepath.Join(web, "node_modules", ".bin"),
		filepath.Join(e2e, "node_modules", "@playwright", "test"),
	} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(web, ".next", "BUILD_ID"), []byte("fake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "node_modules", ".bin", "next"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, lockfile := range []string{
		filepath.Join(web, "pnpm-lock.yaml"),
		filepath.Join(e2e, "pnpm-lock.yaml"),
	} {
		if err := os.WriteFile(lockfile, []byte("lockfileVersion: '9.0'\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	browserPath := filepath.Join(t.TempDir(), "browser")
	if !missingBrowser {
		if err := os.WriteFile(browserPath, []byte("fake browser\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("SMT_FAKE_WEB_BROWSER_PATH", browserPath)
}

func installFakeWebE2EASDF(t *testing.T, testStatus int, nodeFailure bool) string {
	t.Helper()
	directory := t.TempDir()
	logPath := filepath.Join(directory, "asdf.log")
	script := `#!/bin/sh
set -eu
log="$SMT_FAKE_WEB_E2E_LOG"
for arg in "$@"; do printf 'arg=%s\n' "$arg" >> "$log"; done
if [ "${1:-}" != "exec" ]; then
  exit 127
fi
case "${2:-}" in
  node)
    if [ "${SMT_FAKE_WEB_NODE_FAILURE:-0}" = "1" ]; then
      exit 127
    fi
    printf '%s\n' "$SMT_FAKE_WEB_BROWSER_PATH"
    ;;
	  pnpm)
	    case "${3:-}" in
	      --version)
	        printf '10.0.0\n'
	        ;;
	      run)
	        if [ "${4:-}" != "test" ]; then
	          exit 127
	        fi
	        printf 'FAKE_PLAYWRIGHT_OUTPUT\n'
	        exit "${SMT_FAKE_WEB_PNPM_TEST_STATUS:-0}"
        ;;
    esac
    ;;
  *)
    exit 127
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(directory, "asdf"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("SMT_FAKE_WEB_E2E_LOG", logPath)
	t.Setenv("SMT_FAKE_WEB_PNPM_TEST_STATUS", fmt.Sprint(testStatus))
	if nodeFailure {
		t.Setenv("SMT_FAKE_WEB_NODE_FAILURE", "1")
	} else {
		t.Setenv("SMT_FAKE_WEB_NODE_FAILURE", "0")
	}
	return logPath
}

func runGeneratedWebE2E(t *testing.T, destination string, args ...string) ([]byte, error) {
	t.Helper()
	runner := filepath.Join(destination, "e2e", "web", "run.sh")
	return exec.Command("sh", append([]string{runner}, args...)...).CombinedOutput()
}

func readWebE2EReport(t *testing.T, destination, name string) string {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(destination, "e2e", "web", "reports", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}
