package apply

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootTaskfileDispatchesOnlySelectedComponents(t *testing.T) {
	tests := []struct {
		name      string
		web       bool
		mobile    bool
		api       bool
		database  bool
		fast      []string
		full      []string
		forbidden []string
	}{
		{
			name:      "web only",
			web:       true,
			fast:      []string{"cd \"{{.ROOT_DIR}}/web-app\" && asdf exec pnpm run verify:fast"},
			full:      []string{"cd \"{{.ROOT_DIR}}/web-app\" && asdf exec pnpm run verify"},
			forbidden: []string{"mobile-app", "apis", "database"},
		},
		{
			name:      "mobile only",
			mobile:    true,
			fast:      []string{"cd \"{{.ROOT_DIR}}/mobile-app\" && asdf exec dart format", "cd \"{{.ROOT_DIR}}/mobile-app\" && asdf exec flutter analyze"},
			full:      []string{"cd \"{{.ROOT_DIR}}/mobile-app\" && asdf exec flutter test"},
			forbidden: []string{"web-app", "apis", "database", "Taskfile"},
		},
		{
			name:      "api only",
			api:       true,
			fast:      []string{"cd \"{{.ROOT_DIR}}/apis\" && task verify:fast"},
			full:      []string{"cd \"{{.ROOT_DIR}}/apis\" && task verify"},
			forbidden: []string{"web-app", "mobile-app", "database"},
		},
		{
			name:      "database only",
			database:  true,
			full:      []string{"cd \"{{.ROOT_DIR}}/database\" && task verify"},
			forbidden: []string{"web-app", "mobile-app", "apis"},
		},
		{
			name: "web and api",
			web:  true, api: true,
			fast: []string{
				"cd \"{{.ROOT_DIR}}/web-app\" && asdf exec pnpm run verify:fast",
				"cd \"{{.ROOT_DIR}}/apis\" && task verify:fast",
			},
			full: []string{
				"cd \"{{.ROOT_DIR}}/web-app\" && asdf exec pnpm run verify",
				"cd \"{{.ROOT_DIR}}/apis\" && task verify",
			},
			forbidden: []string{"mobile-app", "database"},
		},
		{
			name: "all components",
			web:  true, mobile: true, api: true, database: true,
			fast: []string{
				"cd \"{{.ROOT_DIR}}/web-app\" && asdf exec pnpm run verify:fast",
				"cd \"{{.ROOT_DIR}}/mobile-app\" && asdf exec dart format",
				"cd \"{{.ROOT_DIR}}/apis\" && task verify:fast",
			},
			full: []string{
				"cd \"{{.ROOT_DIR}}/web-app\" && asdf exec pnpm run verify",
				"cd \"{{.ROOT_DIR}}/mobile-app\" && asdf exec flutter test",
				"cd \"{{.ROOT_DIR}}/apis\" && task verify",
				"cd \"{{.ROOT_DIR}}/database\" && task verify",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := rootTaskfileForSelection(tt.web, tt.mobile, tt.api, tt.database)
			if strings.Count(text, "version: '3'") != 1 || strings.Count(text, "tasks:\n") != 1 {
				t.Fatalf("taskfile has duplicate YAML sections:\n%s", text)
			}
			if !strings.Contains(text, "\n  verify:fast:\n") || !strings.Contains(text, "\n  verify:\n") {
				t.Fatalf("taskfile is missing aggregate verification tasks:\n%s", text)
			}
			for _, marker := range append(tt.fast, tt.full...) {
				if !strings.Contains(text, marker) {
					t.Errorf("taskfile missing selected dispatch %q:\n%s", marker, text)
				}
			}
			for _, marker := range tt.forbidden {
				if strings.Contains(text, marker) {
					t.Errorf("taskfile contains unselected marker %q:\n%s", marker, text)
				}
			}
			if tt.database && strings.Contains(taskfileTaskBlock(text, "verify:fast"), "database") {
				t.Errorf("fast task invokes the Database lane:\n%s", text)
			}
		})
	}
}

func TestRootTaskfileParsesWithTask(t *testing.T) {
	taskPath := generatedTaskBinary(t)
	destination := t.TempDir()
	if err := os.WriteFile(filepath.Join(destination, "Taskfile.yml"), []byte(rootTaskfileForSelection(true, true, true, true)), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(taskPath, "--list-all")
	command.Dir = destination
	command.Env = generatedTaskEnvironment()
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Task could not parse the generated root Taskfile: %v\n%s", err, output)
	}
	for _, marker := range []string{"verify:fast", "verify", "compose:up"} {
		if !strings.Contains(string(output), marker) {
			t.Fatalf("Task listing is missing %q:\n%s", marker, output)
		}
	}
}

func TestRootTaskfileOrdersSelectedVerificationAndPreservesE2E(t *testing.T) {
	base := rootTaskfileForSelection(true, true, true, true)
	merged := mergeTaskfile(base, e2eOrchestrationFiles(true, true)["Taskfile.yml"])
	for _, marker := range []string{"e2e:web:", "e2e:mobile:", "e2e:verify:", "compose:up:"} {
		if !strings.Contains(merged, marker) {
			t.Fatalf("merged taskfile missing %q:\n%s", marker, merged)
		}
	}
	if strings.Count(merged, "version: '3'") != 1 || strings.Count(merged, "tasks:\n") != 1 {
		t.Fatalf("merged taskfile duplicated YAML sections:\n%s", merged)
	}
	if !strings.Contains(merged, "\n  verify:\n") || strings.Contains(merged, "\n  verify:\n    cmds:\n          set +e") {
		t.Fatalf("E2E coordinator overwrote the root aggregate verify task:\n%s", merged)
	}
	order := []string{
		"cd \"{{.ROOT_DIR}}/web-app\" && asdf exec pnpm run verify:fast",
		"cd \"{{.ROOT_DIR}}/mobile-app\" && asdf exec dart format",
		"cd \"{{.ROOT_DIR}}/apis\" && task verify:fast",
		"cd \"{{.ROOT_DIR}}/database\" && task verify",
	}
	last := -1
	for _, marker := range order {
		index := strings.Index(merged, marker)
		if index <= last {
			t.Fatalf("verification dispatch order is not deterministic at %q:\n%s", marker, merged)
		}
		last = index
	}
}

func TestApplyMobileOnlyEmitsRootVerificationTaskfileWithoutMobileTaskfile(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyWorkspace(t, destination, mobileOnlyBlueprintBytes())

	contents, err := os.ReadFile(filepath.Join(destination, "Taskfile.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, marker := range []string{
		"verify:fast:",
		"verify:",
		"asdf exec dart format",
		"asdf exec flutter analyze",
		"asdf exec flutter test",
	} {
		if !strings.Contains(text, marker) {
			t.Fatalf("Mobile-only root Taskfile missing %q:\n%s", marker, text)
		}
	}
	if _, err := os.Stat(filepath.Join(destination, "mobile-app", "Taskfile.yml")); !os.IsNotExist(err) {
		t.Fatalf("Mobile output contains a child Taskfile: %v", err)
	}
}

func mobileOnlyBlueprintBytes() []byte {
	return []byte(`version: 1
workspace: {ai_assist: codex, stack: {mobile: flutter}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, mobile]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: mobile, path: mobile-app, component: mobile, technology: flutter, scope: mobile, modules: [mobile], remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`)
}
