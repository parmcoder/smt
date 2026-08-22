package apply

import (
	"encoding/json"
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
			"npm ci --ignore-scripts --no-audit --no-fund",
			"USER nextjs",
			`CMD ["node", "node_modules/next/dist/bin/next", "start"]`,
		},
		"package-lock.json": {
			`"lockfileVersion": 3`,
			`"packages"`,
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

	var lockfile struct {
		LockfileVersion int                        `json:"lockfileVersion"`
		Packages        map[string]json.RawMessage `json:"packages"`
	}
	contents, err := os.ReadFile(filepath.Join(web, "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(contents, &lockfile); err != nil {
		t.Fatalf("decode generated package-lock.json: %v", err)
	}
	if lockfile.LockfileVersion != 3 || len(lockfile.Packages) == 0 {
		t.Fatalf("generated package-lock.json=%s, want lockfileVersion 3 with package entries", contents)
	}
}

func TestWebApplyRuntimeReadmeDocumentsOperationalContract(t *testing.T) {
	destination := applyWebQualityWorkspace(t)
	contents, err := os.ReadFile(filepath.Join(destination, "web-app", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"package-lock.json",
		"asdf exec npm ci",
		"asdf exec npm run build",
		"asdf exec npm start",
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
		"package-lock.json",
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
