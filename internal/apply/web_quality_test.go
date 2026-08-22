package apply

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

func TestWebApplyGeneratesQualitySuiteAndPatchesManifest(t *testing.T) {
	destination := applyWebQualityWorkspace(t)

	packageJSON := readWebQualityPackageJSON(t, destination)
	if packageJSON.Name != "CLI package" {
		t.Fatalf("package name=%q, want CLI package preserved", packageJSON.Name)
	}
	if packageJSON.Dependencies["next"] != "16.2.9" {
		t.Fatalf("next dependency=%q, want CLI runtime dependency preserved", packageJSON.Dependencies["next"])
	}
	if packageJSON.Scripts["dev"] != "next dev" {
		t.Fatalf("dev script=%q, want CLI script preserved", packageJSON.Scripts["dev"])
	}

	wantScripts := map[string]string{
		"format:check": "prettier --check .",
		"format:write": "prettier --write .",
		"lint":         "eslint . --max-warnings=0",
		"typecheck":    "tsc --noEmit",
		"test":         "vitest run",
		"build":        "next build",
		"start":        "next start",
		"test:e2e":     "playwright test",
	}
	for name, want := range wantScripts {
		if packageJSON.Scripts[name] != want {
			t.Errorf("script %q=%q, want %q", name, packageJSON.Scripts[name], want)
		}
	}

	wantDependencies := map[string]string{
		"@playwright/test":          "1.62.1",
		"@testing-library/dom":      "10.4.1",
		"@testing-library/jest-dom": "7.0.1",
		"@testing-library/react":    "16.3.2",
		"@vitejs/plugin-react":      "6.1.0",
		"eslint-config-prettier":    "10.1.8",
		"jsdom":                     "30.0.1",
		"prettier":                  "3.9.6",
		"vite-tsconfig-paths":       "6.1.1",
		"vitest":                    "4.1.11",
	}
	for name, want := range wantDependencies {
		if packageJSON.DevDependencies[name] != want {
			t.Errorf("dev dependency %q=%q, want %q", name, packageJSON.DevDependencies[name], want)
		}
	}

	files := map[string][]string{
		"eslint.config.mjs":           {"core-web-vitals", "typescript", "globalIgnores", "eslint-config-prettier"},
		".prettierrc.json":            {"printWidth"},
		".prettierignore":             {"node_modules", ".next", "playwright-report"},
		"vitest.config.ts":            {"jsdom", "vite-tsconfig-paths", "setupFiles"},
		"test/setup.ts":               {"@testing-library/jest-dom/vitest"},
		"test/quality.smoke.test.tsx": {"render", "jsdom", "quality harness"},
		"playwright.config.ts":        {"@playwright/test", "127.0.0.1:3000", "npm run start", "trace"},
	}
	for relative, markers := range files {
		contents, err := os.ReadFile(filepath.Join(destination, "web-app", relative))
		if err != nil {
			t.Fatalf("read generated %s: %v", relative, err)
		}
		for _, marker := range markers {
			if !strings.Contains(string(contents), marker) {
				t.Errorf("generated %s missing %q:\n%s", relative, marker, contents)
			}
		}
	}

	readme, err := os.ReadFile(filepath.Join(destination, "web-app", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"asdf exec npm ci",
		"asdf exec npm run format:check",
		"asdf exec npm run lint",
		"asdf exec npm run typecheck",
		"asdf exec npm run test",
		"asdf exec npm run test:e2e",
	} {
		if !strings.Contains(string(readme), want) {
			t.Errorf("README missing %q:\n%s", want, readme)
		}
	}

	if _, err := os.Stat(filepath.Join(destination, "web-app", "package-lock.json")); err != nil {
		t.Fatalf("Apply did not emit package-lock.json: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "web-app", "e2e")); !os.IsNotExist(err) {
		t.Fatalf("Apply emitted Web-local E2E specs: %v", err)
	}
}

func TestWebApplyQualityOutputIsDeterministic(t *testing.T) {
	var outputs [2]map[string][]byte
	for i := range outputs {
		destination := applyWebQualityWorkspace(t)
		outputs[i] = make(map[string][]byte)
		for _, relative := range webQualityGeneratedFiles() {
			contents, err := os.ReadFile(filepath.Join(destination, "web-app", relative))
			if err != nil {
				t.Fatal(err)
			}
			outputs[i][relative] = contents
		}
	}
	if !reflect.DeepEqual(outputs[0], outputs[1]) {
		t.Fatalf("Web quality output differs across fresh destinations:\nfirst=%#v\nsecond=%#v", outputs[0], outputs[1])
	}
}

func TestWebApplyQualityPatchFailureLeavesDestinationUnpublished(t *testing.T) {
	original := runNextCreate
	t.Cleanup(func() { runNextCreate = original })
	runNextCreate = func(_ context.Context, _ string, args []string) ([]byte, error) {
		stagedWeb := args[4]
		if err := os.MkdirAll(stagedWeb, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(stagedWeb, "package.json"), []byte("{"), 0o644); err != nil {
			return nil, err
		}
		return nil, nil
	}

	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(blueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	err = service.Apply(context.Background(), destination, blueprintBytes())
	if err == nil || !strings.Contains(err.Error(), "Web quality") {
		t.Fatalf("Apply() error=%v, want Web quality manifest error", err)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("published destination exists after Web quality failure: %v", statErr)
	}
}

func TestNonWebApplyEmitsNoWebQualityArtifacts(t *testing.T) {
	installFakeNextASDF(t, false)
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	raw := nonWebBlueprintBytes()
	cfg, err := config.LoadBytes(raw, filepath.Join(parent, "blueprint.yaml"))
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
	if _, err := os.Lstat(filepath.Join(destination, "web-app")); !os.IsNotExist(err) {
		t.Fatalf("non-Web Apply emitted web-app: %v", err)
	}
}

type webQualityPackageJSON struct {
	Name            string            `json:"name"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Scripts         map[string]string `json:"scripts"`
}

func applyWebQualityWorkspace(t *testing.T) string {
	t.Helper()
	installFakeNextASDF(t, false)
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	raw := blueprintBytes()
	cfg, err := config.LoadBytes(raw, filepath.Join(parent, "blueprint.yaml"))
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
	return destination
}

func readWebQualityPackageJSON(t *testing.T, destination string) webQualityPackageJSON {
	t.Helper()
	contents, err := os.ReadFile(filepath.Join(destination, "web-app", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packageJSON webQualityPackageJSON
	if err := json.Unmarshal(contents, &packageJSON); err != nil {
		t.Fatalf("decode generated package.json: %v\n%s", err, contents)
	}
	return packageJSON
}

func webQualityGeneratedFiles() []string {
	return []string{
		"package.json",
		"eslint.config.mjs",
		".prettierrc.json",
		".prettierignore",
		"vitest.config.ts",
		"test/setup.ts",
		"test/quality.smoke.test.tsx",
		"playwright.config.ts",
		"README.md",
	}
}
