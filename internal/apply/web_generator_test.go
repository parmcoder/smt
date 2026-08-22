package apply

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

func TestWebApplyInvokesNextInitializerInStagedOrder(t *testing.T) {
	logPath := installFakeNextASDF(t, false)
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

	cwd, args, toolVersions := readFakeNextASDFInvocation(t, logPath)
	if len(args) == 0 {
		t.Fatal("Next.js CLI was not invoked")
	}
	if len(args) < 5 {
		t.Fatalf("Next.js argv=%q, want destination and flags", args)
	}
	stagedWeb := args[4]
	wantArgs := []string{
		"exec", "npx", "--yes", "create-next-app@16.2.9", stagedWeb,
		"--typescript", "--eslint", "--app", "--empty", "--tailwind",
		"--use-npm", "--skip-install", "--disable-git", "--agents-md",
		"--import-alias=@/*",
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("Next.js argv=%q, want %q", args, wantArgs)
	}
	if cwd != filepath.Dir(stagedWeb) {
		t.Fatalf("Next.js cwd=%q, want staged root %q", cwd, filepath.Dir(stagedWeb))
	}
	if stagedWeb == filepath.Join(destination, "web-app") {
		t.Fatalf("Next.js received published destination %q", stagedWeb)
	}
	if !strings.Contains(toolVersions, "nodejs 24.18.0") {
		t.Fatalf("Next.js ran before root .tool-versions was written: %q", toolVersions)
	}
}

func TestWebApplyPreservesCLIOutputMergesIgnoreAndDoesNotCreateLockfile(t *testing.T) {
	logPath := installFakeNextASDF(t, false)
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

	web := filepath.Join(destination, "web-app")
	readme, err := os.ReadFile(filepath.Join(web, "README.md"))
	if err != nil || !strings.Contains(string(readme), "CLI Web output") {
		t.Fatalf("CLI README was not preserved: %q, err=%v", readme, err)
	}
	for _, want := range []string{"asdf exec npm install", "asdf exec npm run dev"} {
		if !strings.Contains(string(readme), want) {
			t.Fatalf("preserved CLI README missing SMT guidance %q: %q", want, readme)
		}
	}
	packageJSON, err := os.ReadFile(filepath.Join(web, "package.json"))
	if err != nil || !strings.Contains(string(packageJSON), "CLI package") {
		t.Fatalf("CLI package.json was not preserved: %q, err=%v", packageJSON, err)
	}
	ignore, err := os.ReadFile(filepath.Join(web, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# CLI ignore", "cli-only.txt", "node_modules/", ".next/", ".env", "**/.DS_Store"} {
		if !strings.Contains(string(ignore), want) {
			t.Fatalf("Web .gitignore missing %q: %q", want, ignore)
		}
	}
	if _, err := os.Lstat(filepath.Join(web, "package-lock.json")); !os.IsNotExist(err) {
		t.Fatalf("Apply emitted package-lock.json: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(web, ".git", "nested")); !os.IsNotExist(err) {
		t.Fatalf("nested CLI .git repository survived: %v", err)
	}
	for relative, want := range map[string]string{
		"app/page.tsx":       "CLI App Router output",
		"tailwind.config.ts": "CLI Tailwind output",
		"AGENTS.md":          "CLI Web agent instructions",
	} {
		contents, err := os.ReadFile(filepath.Join(web, relative))
		if err != nil || !strings.Contains(string(contents), want) {
			t.Fatalf("CLI output %s was not preserved: %q, err=%v", relative, contents, err)
		}
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(log), "cwd="); got != 1 {
		t.Fatalf("Apply invoked asdf %d times, want only the pinned initializer: %s", got, log)
	}
	if strings.Contains(string(log), "arg=npm\n") || strings.Contains(string(log), "arg=install\n") {
		t.Fatalf("Apply invoked npm install: %s", log)
	}
}

func TestWebInitializerFailurePreservesOutputAndPublishesNothing(t *testing.T) {
	installFakeNextASDF(t, true)
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
	if err == nil || !strings.Contains(err.Error(), "asdf install nodejs 24.18.0") || !strings.Contains(err.Error(), "asdf current nodejs") || !strings.Contains(err.Error(), "asdf exec npx --yes create-next-app@16.2.9 --help") {
		t.Fatalf("Apply() error=%v, want actionable Node/asdf guidance", err)
	}
	if !strings.Contains(err.Error(), "NEXT_CLI_STDOUT") || !strings.Contains(err.Error(), "NEXT_CLI_STDERR") {
		t.Fatalf("Apply() error=%v, want preserved CLI output", err)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("published destination exists after Next.js failure: %v", statErr)
	}
	assertNoStage(t, parent)
}

func TestNonWebApplyDoesNotInvokeNextInitializerOrEmitWebOutput(t *testing.T) {
	logPath := installFakeNextASDF(t, false)
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
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Fatalf("non-Web Apply invoked asdf: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "web-app")); !os.IsNotExist(err) {
		t.Fatalf("non-Web Apply emitted Web output: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(destination, "agents", "web_worker.toml")); !os.IsNotExist(err) {
		t.Fatalf("non-Web Apply emitted Web worker output: %v", err)
	}
}

func TestWebApplyIsDeterministicAcrossFreshDestinations(t *testing.T) {
	installFakeNextASDF(t, false)
	raw := blueprintBytes()
	var outputs [2]map[string][]byte
	for i := range outputs {
		parent := t.TempDir()
		destination := filepath.Join(parent, "workspace")
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
		outputs[i] = make(map[string][]byte)
		for _, relative := range []string{"README.md", ".gitignore", "package.json"} {
			contents, err := os.ReadFile(filepath.Join(destination, "web-app", relative))
			if err != nil {
				t.Fatal(err)
			}
			outputs[i][relative] = contents
		}
	}
	if !reflect.DeepEqual(outputs[0], outputs[1]) {
		t.Fatalf("Web output differs across fresh destinations: %#v vs %#v", outputs[0], outputs[1])
	}
}

func TestGeneratedWebWorkerManifestAndRoutingAreWebSpecific(t *testing.T) {
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
	manifest, err := os.ReadFile(filepath.Join(destination, "agents", "web_worker.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Next.js", "TypeScript", "build-web-apps:react-best-practices", "build-web-apps:frontend-testing-debugging"} {
		if !strings.Contains(string(manifest), want) {
			t.Fatalf("Web worker manifest missing %q: %s", want, manifest)
		}
	}
	routing, err := os.ReadFile(filepath.Join(destination, "agents", "work_manager.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(routing), "web_worker") {
		t.Fatalf("work_manager routing omits web_worker: %s", routing)
	}
}

func installFakeNextASDF(t *testing.T, fail bool) string {
	t.Helper()
	directory := t.TempDir()
	logPath := filepath.Join(directory, "asdf.log")
	script := `#!/bin/sh
set -eu
log="$SMT_FAKE_NEXT_ASDF_LOG"
printf 'cwd=%s\n' "$PWD" >> "$log"
if [ -f "$PWD/.tool-versions" ]; then
  printf 'tool-versions=%s\n' "$(tr '\n' '|' < "$PWD/.tool-versions")" >> "$log"
else
  printf 'tool-versions=<missing>\n' >> "$log"
fi
for arg in "$@"; do
  printf 'arg=%s\n' "$arg" >> "$log"
done
if [ "${1:-}" != "exec" ] || [ "${2:-}" != "npx" ]; then
  exit 0
fi
destination="$5"
if [ "${SMT_FAKE_NEXT_FAIL:-0}" = "1" ]; then
  mkdir -p "$destination/partial/.git"
  printf 'partial\n' > "$destination/partial/marker"
  printf 'NEXT_CLI_STDOUT\n'
  printf 'NEXT_CLI_STDERR\n' >&2
  exit 17
fi
mkdir -p "$destination/.git/nested" "$destination/generated/.git" "$destination/app"
printf 'CLI Web output\n' > "$destination/README.md"
printf '# CLI ignore\ncli-only.txt\n' > "$destination/.gitignore"
printf '{"name":"CLI package"}\n' > "$destination/package.json"
printf '{"name":"should not survive Apply"}\n' > "$destination/package-lock.json"
printf 'created by Next.js CLI\n' > "$destination/generated/cli-owned.txt"
printf 'CLI App Router output\n' > "$destination/app/page.tsx"
printf 'CLI Tailwind output\n' > "$destination/tailwind.config.ts"
printf 'CLI Web agent instructions\n' > "$destination/AGENTS.md"
`
	if err := os.WriteFile(filepath.Join(directory, "asdf"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH")+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("SMT_FAKE_NEXT_ASDF_LOG", logPath)
	if fail {
		t.Setenv("SMT_FAKE_NEXT_FAIL", "1")
	} else {
		t.Setenv("SMT_FAKE_NEXT_FAIL", "0")
	}
	return logPath
}

func readFakeNextASDFInvocation(t *testing.T, logPath string) (string, []string, string) {
	t.Helper()
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var cwd, versions string
	var args []string
	for _, line := range strings.Split(strings.TrimSpace(string(contents)), "\n") {
		switch {
		case strings.HasPrefix(line, "cwd="):
			cwd = strings.TrimPrefix(line, "cwd=")
		case strings.HasPrefix(line, "tool-versions="):
			versions = strings.TrimPrefix(line, "tool-versions=")
		case strings.HasPrefix(line, "arg="):
			args = append(args, strings.TrimPrefix(line, "arg="))
		}
	}
	return cwd, args, versions
}

func nonWebBlueprintBytes() []byte {
	return []byte(`version: 1
workspace: {ai_assist: codex, stack: {api: go}}
commit: {types: [feat, fix, refactor, perf, test, docs, build, ci, chore, revert], scopes: [repo, api]}
repositories:
  - {id: repo, path: ., scope: repo, remote: {url: ""}}
  - {id: api, path: apis, component: api, technology: go, scope: api, modules: [api], remote: {url: ""}}
workflow:
  policy: {manager: work_manager, implementation: backend_worker, documentation: doc_writer, review_required: true}
  plugins:
    - {source: parmcoder/codex-obsidian, selectors: [codex-obsidian-writer, codex-obsidian-markdown]}
    - {source: parmcoder/godex, selectors: [godex-go-backend]}
`)
}
