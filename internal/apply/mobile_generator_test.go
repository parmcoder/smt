package apply

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parmcoder/smt/internal/config"
)

func TestMobileApplyInvokesFlutterCLIInStagedOrder(t *testing.T) {
	logPath := installFakeASDF(t, false)
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	if err := service.Apply(context.Background(), destination, mobileBlueprintBytes()); err != nil {
		t.Fatal(err)
	}

	cwd, allArgs, toolVersions := readFakeASDFInvocation(t, logPath)
	flutterStart := -1
	for i := 0; i+1 < len(allArgs); i++ {
		if allArgs[i] == "exec" && allArgs[i+1] == "flutter" {
			flutterStart = i
			break
		}
	}
	if flutterStart == -1 {
		t.Fatal("Flutter CLI was not invoked")
	}
	args := allArgs[flutterStart:]
	stagedMobile := args[len(args)-1]
	wantArgs := []string{
		"exec",
		"flutter",
		"--suppress-analytics",
		"create",
		"--empty",
		"--no-pub",
		"--platforms=android,ios",
		"--org=com.example.smt",
		"--project-name=smt_mobile",
		"--description=A provider-neutral SMT Flutter mobile starter.",
		stagedMobile,
	}
	if fmt.Sprint(args) != fmt.Sprint(wantArgs) {
		t.Fatalf("Flutter argv=%q, want %q", args, wantArgs)
	}
	if cwd != filepath.Dir(stagedMobile) {
		t.Fatalf("Flutter cwd=%q, want staged root %q", cwd, filepath.Dir(stagedMobile))
	}
	if stagedMobile == filepath.Join(destination, "mobile-app") {
		t.Fatalf("Flutter received published destination %q", stagedMobile)
	}
	if !strings.Contains(toolVersions, "flutter 3.44.9-stable") {
		t.Fatalf("Flutter ran before root .tool-versions was written: %q", toolVersions)
	}

	for _, relative := range []string{"android/cli-owned.txt", "ios/cli-owned.txt"} {
		if _, err := os.Stat(filepath.Join(destination, "mobile-app", relative)); err != nil {
			t.Fatalf("CLI-owned platform output %s missing: %v", relative, err)
		}
	}
	for _, relative := range []string{"android/gradlew", "ios/Runner.xcodeproj/project.pbxproj"} {
		if _, err := os.Stat(filepath.Join(destination, "mobile-app", relative)); !os.IsNotExist(err) {
			t.Fatalf("static platform fallback output %s exists: %v", relative, err)
		}
	}
}

func TestNonMobileApplyDoesNotInvokeFlutterCLI(t *testing.T) {
	logPath := installFakeASDF(t, false)
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
	if err := service.Apply(context.Background(), destination, blueprintBytes()); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(contents), "arg=npx\n") || strings.Contains(string(contents), "arg=flutter\n") {
		t.Fatalf("non-Mobile Apply invoked the wrong asdf tool: %s", contents)
	}
}

func TestFlutterCLIFailureLeavesNoPublishedWorkspaceAndGuidesAsdf(t *testing.T) {
	installFakeASDF(t, true)
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	err = service.Apply(context.Background(), destination, mobileBlueprintBytes())
	if err == nil || !strings.Contains(err.Error(), "asdf install flutter 3.44.9-stable") || !strings.Contains(err.Error(), "asdf current flutter") {
		t.Fatalf("Apply() error=%v, want actionable asdf guidance", err)
	}
	if _, statErr := os.Lstat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("published destination exists after Flutter failure: %v", statErr)
	}
	assertNoStage(t, parent)
}

func TestMobileApplyPreservesFlutterCLIOutput(t *testing.T) {
	installFakeASDF(t, false)
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	if err := service.Apply(context.Background(), destination, mobileBlueprintBytes()); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(destination, "mobile-app")
	wantFiles := map[string]string{
		"pubspec.yaml":          "name: smt_mobile\ndescription: Generated by fake Flutter CLI.\npublish_to: \"none\"\nversion: 0.1.0+1\n\nenvironment:\n  sdk: \">=3.12.0 <4.0.0\"\n  flutter: \">=3.44.9\"\n\ndependencies:\n  flutter:\n    sdk: flutter\n\ndev_dependencies:\n  flutter_lints: ^5.0.0\n  flutter_test:\n    sdk: flutter\n  integration_test:\n    sdk: flutter\n\nflutter:\n  uses-material-design: true\n",
		"pubspec.lock":          "lock generated by Flutter CLI\n",
		"analysis_options.yaml": "include: cli-analysis.yaml\n",
		"android/cli-owned.txt": "created by Flutter CLI\n",
		"ios/cli-owned.txt":     "created by Flutter CLI\n",
	}
	for relative, want := range wantFiles {
		got, err := os.ReadFile(filepath.Join(child, relative))
		if err != nil {
			t.Fatalf("CLI-owned output %s missing: %v", relative, err)
		}
		if string(got) != want {
			t.Fatalf("CLI-owned output %s=%q, want %q", relative, got, want)
		}
	}
}

func TestMobileApplyGeneratesVerificationContract(t *testing.T) {
	installFakeASDF(t, false)
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	if err := service.Apply(context.Background(), destination, mobileBlueprintBytes()); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(destination, "mobile-app")
	for _, relative := range []string{
		"lib/src/config.dart",
		"test/config_test.dart",
		"test/widget_test.dart",
		"integration_test/app_test.dart",
	} {
		if _, err := os.Stat(filepath.Join(child, relative)); err != nil {
			t.Fatalf("verification file %s missing: %v", relative, err)
		}
	}
	main, err := os.ReadFile(filepath.Join(child, "lib", "main.dart"))
	if err != nil {
		t.Fatal(err)
	}
	configSource, err := os.ReadFile(filepath.Join(child, "lib", "src", "config.dart"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(configSource), "SMT_API_BASE_URL") {
		t.Fatalf("generated config.dart missing SMT_API_BASE_URL:\n%s", configSource)
	}
	for _, marker := range []string{"mobile-home", "api-status"} {
		if !strings.Contains(string(main), marker) {
			t.Fatalf("generated main.dart missing %q:\n%s", marker, main)
		}
	}
	widget, err := os.ReadFile(filepath.Join(child, "test", "widget_test.dart"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"mobile-home", "api-status", "testWidgets"} {
		if !strings.Contains(string(widget), marker) {
			t.Fatalf("generated widget test missing %q:\n%s", marker, widget)
		}
	}
	integration, err := os.ReadFile(filepath.Join(child, "integration_test", "app_test.dart"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"IntegrationTestWidgetsFlutterBinding", "mobile-home", "api-status"} {
		if !strings.Contains(string(integration), marker) {
			t.Fatalf("generated integration test missing %q:\n%s", marker, integration)
		}
	}
	for _, forbidden := range []string{"password", "secret", "CRUD", "postgres", "compose"} {
		if strings.Contains(strings.ToLower(string(main)), strings.ToLower(forbidden)) {
			t.Fatalf("generated main.dart contains forbidden domain/runtime marker %q:\n%s", forbidden, main)
		}
	}
}

func TestMobileApplyGeneratesNativeVerificationLefthookProfile(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyMobileWorkspace(t, destination)

	rootHook, err := os.ReadFile(filepath.Join(destination, "lefthook.yml"))
	if err != nil {
		t.Fatal(err)
	}
	childHook, err := os.ReadFile(filepath.Join(destination, "mobile-app", "lefthook.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for name, hook := range map[string]string{"root": string(rootHook), "mobile": string(childHook)} {
		for _, marker := range []string{
			"commit-msg:",
			"pre-push:",
			"asdf exec dart format --output=none --set-exit-if-changed lib test integration_test",
			"asdf exec flutter analyze",
		} {
			if !strings.Contains(hook, marker) {
				t.Fatalf("%s Lefthook config missing %q:\n%s", name, marker, hook)
			}
		}
		if strings.Contains(hook, "Taskfile") || strings.Contains(hook, "flutter test") {
			t.Fatalf("%s Lefthook fast profile contains a full verification lane:\n%s", name, hook)
		}
	}
	if !strings.Contains(string(rootHook), "cd mobile-app && asdf exec dart format") || !strings.Contains(string(rootHook), "cd mobile-app && asdf exec flutter analyze") {
		t.Fatalf("root Lefthook does not dispatch native Mobile commands from the workspace root:\n%s", rootHook)
	}
	if strings.Contains(string(childHook), "cd mobile-app") {
		t.Fatalf("Mobile child Lefthook must run from its own repository:\n%s", childHook)
	}
	if _, err := os.Stat(filepath.Join(destination, "mobile-app", "Taskfile.yml")); !os.IsNotExist(err) {
		t.Fatalf("Mobile output contains a Taskfile: %v", err)
	}
}

func TestMobileApplyDocumentsNativeVerificationLanes(t *testing.T) {
	installFakeASDF(t, false)
	destination := filepath.Join(t.TempDir(), "workspace")
	applyMobileWorkspace(t, destination)

	readme, err := os.ReadFile(filepath.Join(destination, "mobile-app", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{
		"## Verification",
		"asdf exec dart format --output=none --set-exit-if-changed lib test integration_test",
		"asdf exec flutter analyze",
		"asdf exec flutter test",
		"asdf exec flutter test integration_test/app_test.dart -d <device-id>",
		"asdf exec flutter build apk --debug",
		"asdf exec flutter build ios --debug --no-codesign",
		"unavailable",
	} {
		if !strings.Contains(string(readme), marker) {
			t.Fatalf("Mobile README missing verification marker %q:\n%s", marker, readme)
		}
	}
}

func TestMobileApplyVerificationContractIsDeterministic(t *testing.T) {
	installFakeASDF(t, false)
	parent := t.TempDir()
	first := filepath.Join(parent, "first")
	second := filepath.Join(parent, "second")
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	for _, destination := range []string{first, second} {
		if err := service.Apply(context.Background(), destination, mobileBlueprintBytes()); err != nil {
			t.Fatal(err)
		}
	}
	for _, relative := range []string{
		"lib/main.dart",
		"lib/src/config.dart",
		"test/config_test.dart",
		"test/widget_test.dart",
		"integration_test/app_test.dart",
	} {
		left, err := os.ReadFile(filepath.Join(first, "mobile-app", relative))
		if err != nil {
			t.Fatal(err)
		}
		right, err := os.ReadFile(filepath.Join(second, "mobile-app", relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(left) != string(right) {
			t.Fatalf("generated %s is not deterministic", relative)
		}
	}
}

func TestMobileApplyRemovesFlutterIDEFilesBeforePublish(t *testing.T) {
	installFakeASDF(t, false)
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	if err := service.Apply(context.Background(), destination, mobileBlueprintBytes()); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(destination, "mobile-app")
	if _, err := os.Stat(filepath.Join(child, ".idea")); !os.IsNotExist(err) {
		t.Fatalf("Flutter IDE state survived publish: %v", err)
	}
	for _, relative := range []string{"pubspec.yaml", "pubspec.lock", "lib/main.dart", "android/cli-owned.txt", "ios/cli-owned.txt"} {
		if _, err := os.Stat(filepath.Join(child, relative)); err != nil {
			t.Fatalf("CLI output %s was removed with .idea: %v", relative, err)
		}
	}
}

func TestRealFlutterCreatePubGetAndAnalyzeWhenOptedIn(t *testing.T) {
	if os.Getenv("SMT_REAL_FLUTTER") != "1" {
		t.Skip("SMT_REAL_FLUTTER is not set; real Flutter verification is an explicit local lane")
	}
	if _, err := exec.LookPath("asdf"); err != nil {
		t.Fatalf("asdf is required for the opted-in Flutter verification lane: %v", err)
	}
	parent := t.TempDir()
	destination := filepath.Join(parent, "workspace")
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(parent, "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	if err := service.Apply(context.Background(), destination, mobileBlueprintBytes()); err != nil {
		t.Fatalf("real Flutter Apply failed; install the pinned SDK with `asdf install flutter 3.44.9-stable` and retry: %v", err)
	}
	child := filepath.Join(destination, "mobile-app")
	for _, args := range [][]string{{"exec", "flutter", "--version"}, {"exec", "flutter", "pub", "get"}} {
		command := exec.CommandContext(context.Background(), "asdf", args...)
		command.Dir = child
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("asdf %v failed: %v: %s", args, err, output)
		}
	}
	where := exec.CommandContext(context.Background(), "asdf", "where", "flutter")
	where.Dir = child
	flutterRoot, err := where.Output()
	if err != nil {
		t.Fatalf("asdf where flutter failed: %v", err)
	}
	dart := filepath.Join(strings.TrimSpace(string(flutterRoot)), "bin", "cache", "dart-sdk", "bin", "dart")
	format := exec.CommandContext(context.Background(), dart, "format", "--output=none", "--set-exit-if-changed", "lib", "test", "integration_test")
	format.Dir = child
	if output, err := format.CombinedOutput(); err != nil {
		t.Fatalf("dart format failed: %v: %s", err, output)
	}
	for _, args := range [][]string{{"exec", "flutter", "analyze"}, {"exec", "flutter", "test"}} {
		command := exec.CommandContext(context.Background(), "asdf", args...)
		command.Dir = child
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("asdf %v failed: %v: %s", args, err, output)
		}
	}
	devices := exec.CommandContext(context.Background(), "asdf", "exec", "flutter", "devices", "--machine")
	devices.Dir = child
	deviceOutput, err := devices.CombinedOutput()
	if err != nil {
		t.Fatalf("flutter devices failed: %v: %s", err, deviceOutput)
	}
	var devicesFound []struct {
		IsSupported    bool   `json:"isSupported"`
		TargetPlatform string `json:"targetPlatform"`
	}
	if err := json.Unmarshal(deviceOutput, &devicesFound); err != nil {
		t.Fatalf("flutter devices returned invalid machine output: %v: %s", err, deviceOutput)
	}
	hasSupportedMobileDevice := false
	for _, device := range devicesFound {
		if device.IsSupported && (strings.HasPrefix(device.TargetPlatform, "android") || strings.HasPrefix(device.TargetPlatform, "ios")) {
			hasSupportedMobileDevice = true
			break
		}
	}
	if !hasSupportedMobileDevice {
		t.Log("unverified integration/device lane: Flutter reported no available targets")
	} else {
		integration := exec.CommandContext(context.Background(), "asdf", "exec", "flutter", "test", "integration_test")
		integration.Dir = child
		if output, err := integration.CombinedOutput(); err != nil {
			t.Fatalf("flutter integration_test failed with an available target: %v: %s", err, output)
		}
	}
	for _, args := range [][]string{{"exec", "flutter", "build", "apk", "--debug"}, {"exec", "flutter", "build", "ios", "--debug", "--no-codesign"}} {
		command := exec.CommandContext(context.Background(), "asdf", args...)
		command.Dir = child
		output, err := command.CombinedOutput()
		if err == nil {
			continue
		}
		lower := strings.ToLower(string(output))
		if strings.Contains(lower, "android sdk") || strings.Contains(lower, "android toolchain") || strings.Contains(lower, "sdkmanager") || strings.Contains(lower, "xcode") || strings.Contains(lower, "cocoapods") || strings.Contains(lower, "code signing") || strings.Contains(lower, "application not configured") {
			t.Logf("unverified %s build lane: %v: %s", strings.Join(args[3:], " "), err, output)
			continue
		}
		t.Fatalf("asdf %v failed: %v: %s", args, err, output)
	}
}

func applyMobileWorkspace(t *testing.T, destination string) {
	t.Helper()
	cfg, err := config.LoadBytes(mobileBlueprintBytes(), filepath.Join(filepath.Dir(destination), "blueprint.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		Config:        *cfg,
		Prerequisites: prerequisiteFunc(func(context.Context) error { return nil }),
		Beads:         initializerFunc(func(context.Context, string) error { return nil }),
	}
	if err := service.Apply(context.Background(), destination, mobileBlueprintBytes()); err != nil {
		t.Fatal(err)
	}
}

func installFakeASDF(t *testing.T, fail bool) string {
	t.Helper()
	directory := t.TempDir()
	logPath := filepath.Join(directory, "asdf.log")
	script := `#!/bin/sh
set -eu
log="$SMT_FAKE_ASDF_LOG"
printf 'cwd=%s\n' "$PWD" >> "$log"
if [ -f "$PWD/.tool-versions" ]; then
  printf 'tool-versions=%s\n' "$(tr '\n' '|' < "$PWD/.tool-versions")" >> "$log"
else
  printf 'tool-versions=<missing>\n' >> "$log"
fi
for arg in "$@"; do
  printf 'arg=%s\n' "$arg" >> "$log"
done
destination=""
if [ "${2:-}" = "npx" ]; then
  destination="$5"
else
  for arg in "$@"; do destination="$arg"; done
fi
if [ "${2:-}" = "npx" ]; then
  mkdir -p "$destination"
  printf 'Generated by fake Next.js CLI.\n' > "$destination/README.md"
  printf '{"name":"smt_web"}\n' > "$destination/package.json"
  exit 0
fi
if [ "${SMT_FAKE_ASDF_FAIL:-0}" = "1" ] && [ "${2:-}" = "flutter" ]; then
  mkdir -p "$destination/lib"
  printf 'partial\n' > "$destination/lib/partial.dart"
  exit 17
fi
mkdir -p "$destination/lib" "$destination/test" "$destination/integration_test" "$destination/android" "$destination/ios" "$destination/.idea/libraries" "$destination/.idea/runConfigurations"
cat > "$destination/pubspec.yaml" <<'EOF'
name: smt_mobile
description: Generated by fake Flutter CLI.
publish_to: "none"
version: 0.1.0+1

environment:
  sdk: ">=3.12.0 <4.0.0"
  flutter: ">=3.44.9"

dependencies:
  flutter:
    sdk: flutter

dev_dependencies:
  flutter_lints: ^5.0.0
  flutter_test:
    sdk: flutter

flutter:
  uses-material-design: true
EOF
printf 'include: package:flutter_lints/flutter.yaml\n' > "$destination/analysis_options.yaml"
printf 'created by Flutter CLI\n' > "$destination/lib/main.dart"
printf 'created by Flutter CLI\n' > "$destination/test/widget_test.dart"
printf 'created by Flutter CLI\n' > "$destination/integration_test/app_test.dart"
printf 'lock generated by Flutter CLI\n' > "$destination/pubspec.lock"
printf 'include: cli-analysis.yaml\n' > "$destination/analysis_options.yaml"
printf 'created by Flutter CLI\n' > "$destination/android/cli-owned.txt"
printf 'created by Flutter CLI\n' > "$destination/ios/cli-owned.txt"
printf 'workspace\n' > "$destination/.idea/workspace.xml"
printf 'modules\n' > "$destination/.idea/modules.xml"
printf 'library\n' > "$destination/.idea/libraries/flutter.xml"
printf 'run configuration\n' > "$destination/.idea/runConfigurations/flutter.xml"
`
	if err := os.WriteFile(filepath.Join(directory, "asdf"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH")+string(os.PathListSeparator)+"/usr/bin:/bin")
	t.Setenv("SMT_FAKE_ASDF_LOG", logPath)
	if fail {
		t.Setenv("SMT_FAKE_ASDF_FAIL", "1")
	} else {
		t.Setenv("SMT_FAKE_ASDF_FAIL", "0")
	}
	return logPath
}

func readFakeASDFInvocation(t *testing.T, logPath string) (string, []string, string) {
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
