package apply

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

func TestApplyBootstrapsWebDependenciesBeforePublication(t *testing.T) {
	destination := applyWebQualityWorkspace(t)
	web := filepath.Join(destination, "web-app")

	if _, err := os.Stat(filepath.Join(web, "pnpm-lock.yaml")); err != nil {
		t.Fatalf("Apply did not publish the Web pnpm lockfile: %v", err)
	}
	if info, err := os.Stat(filepath.Join(web, "node_modules")); err != nil || !info.IsDir() {
		t.Fatalf("Apply did not publish staged Web dependencies: %v", err)
	}
}

func TestApplyRunsDependencySetupOnlyForSelectedProjects(t *testing.T) {
	tests := []struct {
		name string
		raw  []byte
		want []string
	}{
		{name: "web only", raw: blueprintBytes(), want: []string{"exec pnpm install"}},
		{name: "mobile only", raw: mobileOnlyBlueprintBytes(), want: []string{"exec flutter pub get"}},
		{name: "api only", raw: nonWebBlueprintBytes(), want: []string{"exec go mod download"}},
		{name: "database only", raw: databaseBlueprintBytes()},
		{name: "mixed", raw: mobileDatabaseBlueprintBytes(), want: []string{"exec pnpm install", "exec flutter pub get"}},
		{name: "full", raw: fullMobileBlueprintBytes(), want: []string{"exec pnpm install", "exec flutter pub get", "exec go mod download"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(string(tt.raw), "web: nextjs") {
				if strings.Contains(string(tt.raw), "mobile: flutter") {
					installFakeASDF(t, false)
				} else {
					installFakeNextASDF(t, false)
				}
			} else if strings.Contains(string(tt.raw), "mobile: flutter") {
				installFakeASDF(t, false)
			}

			var calls []string
			original := runDependencySetup
			t.Cleanup(func() { runDependencySetup = original })
			runDependencySetup = func(_ context.Context, cwd string, args []string) ([]byte, error) {
				if cwd == "" || strings.Contains(cwd, filepath.Join("workspace", "web-app")) {
					t.Fatalf("dependency setup received a published or empty cwd: %q", cwd)
				}
				if strings.Contains(cwd, ".smt-") == false {
					t.Fatalf("dependency setup cwd=%q, want staging directory", cwd)
				}
				if _, err := os.Stat(filepath.Join(cwd, ".git")); !os.IsNotExist(err) {
					t.Fatalf("dependency setup ran after child repository initialization: %v", err)
				}
				calls = append(calls, strings.Join(args, " "))
				switch strings.Join(args, " ") {
				case "exec pnpm install":
					if _, err := os.Stat(filepath.Join(cwd, "package.json")); err != nil {
						t.Fatalf("Web setup ran before package.json was generated: %v", err)
					}
					if err := os.WriteFile(filepath.Join(cwd, "pnpm-lock.yaml"), []byte("lock\n"), 0o644); err != nil {
						return nil, err
					}
					if err := os.Mkdir(filepath.Join(cwd, "node_modules"), 0o755); err != nil {
						return nil, err
					}
				case "exec flutter pub get":
					if _, err := os.Stat(filepath.Join(cwd, "pubspec.yaml")); err != nil {
						t.Fatalf("Mobile setup ran before pubspec.yaml was generated: %v", err)
					}
					if err := os.WriteFile(filepath.Join(cwd, "pubspec.lock"), []byte("lock\n"), 0o644); err != nil {
						return nil, err
					}
					if err := os.Mkdir(filepath.Join(cwd, ".dart_tool"), 0o755); err != nil {
						return nil, err
					}
				case "exec go mod download":
					if _, err := os.Stat(filepath.Join(cwd, "go.mod")); err != nil {
						t.Fatalf("API setup ran before go.mod was generated: %v", err)
					}
				}
				return nil, nil
			}

			parent := t.TempDir()
			destination := filepath.Join(parent, "workspace")
			cfg, err := config.LoadBytes(tt.raw, filepath.Join(parent, "blueprint.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			service := Service{
				Config:        *cfg,
				Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
				Beads:         initializerFunc(func(context.Context, string) error { return nil }),
			}
			if err := service.Apply(context.Background(), destination, tt.raw); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(calls, tt.want) {
				t.Fatalf("dependency setup calls=%q, want %q", calls, tt.want)
			}
		})
	}
}

func TestApplyDependencySetupFailuresRemainAtomicAndActionable(t *testing.T) {
	tests := []struct {
		name      string
		raw       []byte
		component string
		command   string
		hint      []string
		install   func(*testing.T)
	}{
		{name: "Web", raw: blueprintBytes(), component: "web", command: "asdf exec pnpm install", install: func(t *testing.T) { installFakeNextASDF(t, false) }},
		{name: "Mobile", raw: mobileOnlyBlueprintBytes(), component: "mobile", command: "asdf exec flutter pub get", install: func(t *testing.T) { installFakeASDF(t, false) }},
		{name: "API", raw: nonWebBlueprintBytes(), component: "api", command: "asdf exec go mod download"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.install != nil {
				tt.install(t)
			}
			original := runDependencySetup
			t.Cleanup(func() { runDependencySetup = original })
			runDependencySetup = func(_ context.Context, _ string, args []string) ([]byte, error) {
				if got := "asdf " + strings.Join(args, " "); got != tt.command {
					return nil, errors.New("unexpected setup command: " + got)
				}
				return []byte("SETUP_FAILURE_OUTPUT"), errors.New("exit status 23")
			}

			parent := t.TempDir()
			destination := filepath.Join(parent, "workspace")
			cfg, err := config.LoadBytes(tt.raw, filepath.Join(parent, "blueprint.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			service := Service{
				Config:        *cfg,
				Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
				Beads:         initializerFunc(func(context.Context, string) error { return nil }),
			}
			err = service.Apply(context.Background(), destination, tt.raw)
			if err == nil || !strings.Contains(err.Error(), tt.component) || !strings.Contains(err.Error(), tt.command) || !strings.Contains(err.Error(), "SETUP_FAILURE_OUTPUT") {
				t.Fatalf("Apply() error=%v, want component, command, and captured output", err)
			}
			for _, hint := range tt.hint {
				if !strings.Contains(err.Error(), hint) {
					t.Fatalf("Apply() error=%v, want recovery hint %q", err, hint)
				}
			}
			if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
				t.Fatalf("published destination exists after %s setup failure: %v", tt.component, statErr)
			}
			assertNoStage(t, parent)
		})
	}
}

func TestWebDependencySetupApprovesIgnoredBuildsAndRetriesInstall(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"name":"web"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls []struct {
		cwd  string
		args string
	}
	installAttempts := 0
	original := runDependencySetup
	t.Cleanup(func() { runDependencySetup = original })
	runDependencySetup = func(_ context.Context, cwd string, args []string) ([]byte, error) {
		calls = append(calls, struct {
			cwd  string
			args string
		}{cwd: cwd, args: strings.Join(args, " ")})
		switch strings.Join(args, " ") {
		case "exec pnpm install":
			installAttempts++
			if installAttempts == 1 {
				return []byte("ERR_PNPM_IGNORED_BUILDS: sharp@0.34.5, unrs-resolver@1.12.2"), errors.New("exit status 1")
			}
			if err := os.WriteFile(filepath.Join(cwd, "pnpm-lock.yaml"), []byte("lock\n"), 0o644); err != nil {
				return nil, err
			}
			if err := os.Mkdir(filepath.Join(cwd, "node_modules"), 0o755); err != nil {
				return nil, err
			}
			return []byte("install complete"), nil
		case "exec pnpm approve-builds --all":
			if err := os.WriteFile(filepath.Join(cwd, "pnpm-workspace.yaml"), []byte("allowBuilds:\n  sharp: true\n  unrs-resolver: true\n"), 0o644); err != nil {
				return nil, err
			}
			return []byte("approved"), nil
		default:
			return nil, errors.New("unexpected dependency setup command")
		}
	}

	if err := setupComponentDependencies(context.Background(), component{id: "web"}, root); err != nil {
		t.Fatal(err)
	}
	if got, want := len(calls), 3; got != want {
		t.Fatalf("dependency setup calls=%d, want %d: %#v", got, want, calls)
	}
	if got, want := []string{calls[0].args, calls[1].args, calls[2].args}, []string{
		"exec pnpm install",
		"exec pnpm approve-builds --all",
		"exec pnpm install",
	}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependency setup order=%q, want %q", got, want)
	}
	for _, call := range calls {
		if call.cwd != root {
			t.Fatalf("dependency setup cwd=%q, want %q", call.cwd, root)
		}
	}
	for _, name := range []string{"pnpm-lock.yaml", "pnpm-workspace.yaml", "node_modules"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("setup did not preserve %s: %v", name, err)
		}
	}
}

func TestWebDependencySetupRetryFailureReportsApprovalInsteadOfASDFGuidance(t *testing.T) {
	root := t.TempDir()
	installAttempts := 0
	original := runDependencySetup
	t.Cleanup(func() { runDependencySetup = original })
	runDependencySetup = func(_ context.Context, _ string, args []string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "exec pnpm install":
			installAttempts++
			if installAttempts == 1 {
				return []byte("ERR_PNPM_IGNORED_BUILDS: sharp@0.34.5"), errors.New("exit status 1")
			}
			return []byte("RETRY_FAILURE_OUTPUT"), errors.New("exit status 23")
		case "exec pnpm approve-builds --all":
			return []byte("approved"), nil
		default:
			return nil, errors.New("unexpected dependency setup command")
		}
	}

	err := setupComponentDependencies(context.Background(), component{id: "web"}, root)
	if err == nil || !strings.Contains(err.Error(), "pnpm approve-builds --all") || !strings.Contains(err.Error(), "RETRY_FAILURE_OUTPUT") {
		t.Fatalf("setup error=%v, want approval retry and captured output", err)
	}
	if strings.Contains(err.Error(), "asdf plugin add pnpm") {
		t.Fatalf("setup error=%v, must not mislabel a pnpm build failure as an asdf plugin failure", err)
	}
}

func TestApplyWebApprovalFailureRemainsAtomic(t *testing.T) {
	installFakeNextASDF(t, false)
	original := runDependencySetup
	t.Cleanup(func() { runDependencySetup = original })
	runDependencySetup = func(_ context.Context, _ string, args []string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "exec pnpm install":
			return []byte("ERR_PNPM_IGNORED_BUILDS: sharp@0.34.5"), errors.New("exit status 1")
		case "exec pnpm approve-builds --all":
			return []byte("APPROVAL_FAILURE_OUTPUT"), errors.New("exit status 23")
		default:
			return nil, errors.New("unexpected dependency setup command")
		}
	}

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
	err = service.Apply(context.Background(), destination, raw)
	if err == nil || !strings.Contains(err.Error(), "web") || !strings.Contains(err.Error(), "approve-builds --all") || !strings.Contains(err.Error(), "APPROVAL_FAILURE_OUTPUT") {
		t.Fatalf("Apply() error=%v, want approval command and captured output", err)
	}
	if strings.Contains(err.Error(), "asdf plugin add pnpm") {
		t.Fatalf("Apply() error=%v, must not mislabel pnpm approval failure as an asdf plugin failure", err)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("published destination exists after Web approval failure: %v", statErr)
	}
	assertNoStage(t, parent)
}

func TestApplyPublishesWebBuildApprovalPolicyAfterAutomaticRetry(t *testing.T) {
	installFakeNextASDF(t, false)
	original := runDependencySetup
	t.Cleanup(func() { runDependencySetup = original })
	installAttempts := 0
	runDependencySetup = func(_ context.Context, cwd string, args []string) ([]byte, error) {
		switch strings.Join(args, " ") {
		case "exec pnpm install":
			installAttempts++
			if installAttempts == 1 {
				return []byte("ERR_PNPM_IGNORED_BUILDS: sharp@0.34.5"), errors.New("exit status 1")
			}
			if err := os.WriteFile(filepath.Join(cwd, "pnpm-lock.yaml"), []byte("lock\n"), 0o644); err != nil {
				return nil, err
			}
			if err := os.Mkdir(filepath.Join(cwd, "node_modules"), 0o755); err != nil {
				return nil, err
			}
			return nil, nil
		case "exec pnpm approve-builds --all":
			return nil, os.WriteFile(filepath.Join(cwd, "pnpm-workspace.yaml"), []byte("allowBuilds:\n  sharp: true\n"), 0o644)
		default:
			return nil, errors.New("unexpected dependency setup command")
		}
	}

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
	policy, err := os.ReadFile(filepath.Join(destination, "web-app", "pnpm-workspace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(policy), "sharp: true") {
		t.Fatalf("published pnpm-workspace.yaml=%q, want approved build policy", policy)
	}
}

func TestWebDependencySetupASDFFailureProvidesShortPluginGuidance(t *testing.T) {
	original := runDependencySetup
	t.Cleanup(func() { runDependencySetup = original })
	runDependencySetup = func(_ context.Context, _ string, _ []string) ([]byte, error) {
		return []byte("No version is set for command pnpm"), errors.New("exit status 126")
	}

	err := setupComponentDependencies(context.Background(), component{id: "web"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "asdf plugin add pnpm") || !strings.Contains(err.Error(), "asdf install pnpm 11.24.0") || !strings.Contains(err.Error(), "asdf reshim pnpm 11.24.0") {
		t.Fatalf("setup error=%v, want short plugin recovery guidance", err)
	}
	if strings.Contains(err.Error(), "jonathanmorley/asdf-pnpm.git") {
		t.Fatalf("setup error=%v, want the short asdf plugin command", err)
	}
}

func TestToolVersionsPinsCurrentPnpmVersion(t *testing.T) {
	got := toolVersions([]component{{id: "web"}})
	if !strings.Contains(got, "pnpm 11.24.0\n") {
		t.Fatalf("toolVersions()=%q, want current pnpm pin", got)
	}
}
