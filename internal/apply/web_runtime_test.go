package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWebApplyGeneratesRuntimeSlice(t *testing.T) {
	destination := applyWebQualityWorkspace(t)
	web := filepath.Join(destination, "web-app")

	wants := map[string][]string{
		"app/page.tsx": {
			`data-smt-web-smoke="home"`,
			"SMT Web starter",
			"getAPIConfiguration",
		},
		"app/healthz/route.ts": {
			"export function GET",
			`status: "ok"`,
			"no-store",
		},
		"lib/runtime-config.ts": {
			"API_BASE_URL",
			"http:",
			"https:",
			"username",
		},
		"next.config.ts": {
			`reactStrictMode: true`,
		},
		"Containerfile": {
			"FROM node:24.18.0-alpine",
			"pnpm install --frozen-lockfile --ignore-scripts",
			"USER nextjs",
			`CMD ["pnpm", "start"]`,
		},
		"public/.gitkeep": nil,
	}
	for relative, markers := range wants {
		contents, err := os.ReadFile(filepath.Join(web, relative))
		if err != nil {
			t.Fatalf("read generated runtime file %s: %v", relative, err)
		}
		for _, marker := range markers {
			if !strings.Contains(string(contents), marker) {
				t.Errorf("generated runtime file %s missing %q:\n%s", relative, marker, contents)
			}
		}
	}

	if _, err := os.Stat(filepath.Join(web, "package-lock.json")); !os.IsNotExist(err) {
		t.Fatalf("Apply emitted package-lock.json instead of using pnpm: %v", err)
	}
	for _, relative := range []string{"pnpm-lock.yaml", "node_modules"} {
		if _, err := os.Stat(filepath.Join(web, relative)); err != nil {
			t.Fatalf("Apply did not publish Web dependency output %s: %v", relative, err)
		}
	}
}

func TestWebApplyRuntimeUsesPnpmAndPublishesDependencyInputs(t *testing.T) {
	destination := applyWebQualityWorkspace(t)
	web := filepath.Join(destination, "web-app")

	containerfile, err := os.ReadFile(filepath.Join(web, "Containerfile"))
	if err != nil {
		t.Fatal(err)
	}
	containerText := string(containerfile)
	for _, marker := range []string{
		"corepack enable pnpm",
		"COPY package.json pnpm-lock.yaml ./",
		"pnpm install --frozen-lockfile --ignore-scripts",
		"pnpm run build",
		`CMD ["pnpm", "start"]`,
	} {
		if !strings.Contains(containerText, marker) {
			t.Errorf("Containerfile missing %q:\n%s", marker, containerText)
		}
	}
	if containsStandaloneNpmCommand(containerText) || strings.Contains(containerText, "package-lock.json") {
		t.Errorf("Containerfile contains stale npm lockfile contract:\n%s", containerText)
	}

	readme, err := os.ReadFile(filepath.Join(web, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readmeText := string(readme)
	for _, marker := range []string{
		"asdf exec pnpm install",
		"asdf exec pnpm run build",
		"asdf exec pnpm start",
	} {
		if !strings.Contains(readmeText, marker) {
			t.Errorf("README missing %q:\n%s", marker, readmeText)
		}
	}
	if containsStandaloneNpmCommand(readmeText) || strings.Contains(readmeText, "package-lock.json") {
		t.Errorf("README contains stale npm lockfile contract:\n%s", readmeText)
	}
}

func TestWebApplyRuntimeReadmeDocumentsOperationalContract(t *testing.T) {
	destination := applyWebQualityWorkspace(t)
	contents, err := os.ReadFile(filepath.Join(destination, "web-app", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"asdf exec pnpm install",
		"asdf exec pnpm run build",
		"asdf exec pnpm start",
		"Containerfile",
		"/healthz",
		"API_BASE_URL",
		"data-smt-web-smoke=\"home\"",
		"SIGTERM",
		"outside Compose",
	} {
		if !strings.Contains(string(contents), want) {
			t.Errorf("README missing %q:\n%s", want, contents)
		}
	}
}

func TestWebApplyRuntimeOutputIsDeterministic(t *testing.T) {
	var outputs [2]map[string][]byte
	for i := range outputs {
		destination := applyWebQualityWorkspace(t)
		outputs[i] = make(map[string][]byte)
		for _, relative := range webRuntimeGeneratedFiles() {
			contents, err := os.ReadFile(filepath.Join(destination, "web-app", relative))
			if err != nil {
				t.Fatal(err)
			}
			outputs[i][relative] = contents
		}
	}
	if !mapsEqualBytes(outputs[0], outputs[1]) {
		t.Fatalf("Web runtime output differs across fresh destinations")
	}
}

func TestWebApplyRuntimeOutputHasNoSecretsOrDomainBehavior(t *testing.T) {
	destination := applyWebQualityWorkspace(t)
	for _, relative := range webRuntimeGeneratedFiles() {
		contents, err := os.ReadFile(filepath.Join(destination, "web-app", relative))
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ToLower(string(contents))
		for _, forbidden := range []string{
			"authorization",
			"credential",
			"signing",
			"device farm",
			"cloud integration",
			"crud",
		} {
			if strings.Contains(text, forbidden) {
				t.Errorf("generated runtime file %s contains forbidden behavior %q", relative, forbidden)
			}
		}
	}
}

func webRuntimeGeneratedFiles() []string {
	return []string{
		"app/page.tsx",
		"app/healthz/route.ts",
		"lib/runtime-config.ts",
		"next.config.ts",
		"Containerfile",
		"public/.gitkeep",
	}
}

func mapsEqualBytes(left, right map[string][]byte) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if string(value) != string(right[key]) {
			return false
		}
	}
	return true
}
