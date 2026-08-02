package prereq

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type processCall struct {
	dir  string
	name string
	args []string
}

type fakeRunner struct {
	outputs map[string]Output
	errors  map[string]error
	calls   []processCall
}

func (r *fakeRunner) Run(ctx context.Context, dir, name string, args ...string) (Output, error) {
	r.calls = append(r.calls, processCall{dir: dir, name: name, args: append([]string(nil), args...)})
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	key := commandKey(name, args...)
	return r.outputs[key], r.errors[key]
}

func TestCheckReportsReadyForSelectedPluginsAndRuntimes(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]Output{
		"codex plugin list --json": {Stdout: `{"installed":[{"pluginId":"codex-obsidian@codex-obsidian","installed":true,"enabled":true,"marketplaceSource":{"source":"https://github.com/parmcoder/codex-obsidian.git"}},{"pluginId":"godex@godex","installed":true,"enabled":true,"marketplaceSource":{"source":"https://github.com/parmcoder/godex.git"}}],"available":[{"pluginId":"available@marketplace"}]}`},
		"asdf plugin list":         {Stdout: "golang\nnodejs\n"},
		"asdf list golang":         {Stdout: "  1.26.5\n"},
		"asdf list nodejs":         {Stdout: "*24.18.0\n"},
	}, errors: map[string]error{}}
	result, err := Inspector{Lookup: available, Runner: runner}.Check(context.Background(), Requirements{
		Plugins:  []Plugin{{Source: "parmcoder/codex-obsidian", Selector: "codex-obsidian@codex-obsidian"}, {Source: "parmcoder/godex", Selector: "godex@godex"}},
		Runtimes: []Runtime{{Plugin: "golang", Version: "1.26.5"}, {Plugin: "nodejs", Version: "24.18.0"}},
	})
	if err != nil || !result.Ready() {
		t.Fatalf("Check() result=%#v err=%v", result, err)
	}
	want := []processCall{
		{name: "codex", args: []string{"plugin", "list", "--json"}},
		{name: "asdf", args: []string{"plugin", "list"}},
		{name: "asdf", args: []string{"list", "golang"}},
		{name: "asdf", args: []string{"list", "nodejs"}},
	}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls=%#v want=%#v", runner.calls, want)
	}
	assertInspectionOnly(t, runner.calls)
}

func TestCheckReportsMissingToolsWithHumanOnlyRecheckGuidance(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("not found") }
	runner := &fakeRunner{outputs: map[string]Output{}, errors: map[string]error{}}
	requirements := Requirements{Plugins: []Plugin{{Source: "parmcoder/godex", Selector: "godex@godex"}}, Runtimes: []Runtime{{Plugin: "golang", Version: "1.26.5"}}}
	result, err := Inspector{Lookup: missing, Runner: runner}.Check(context.Background(), requirements)
	if err != nil || result.Ready() || len(runner.calls) != 0 {
		t.Fatalf("first Check() result=%#v calls=%#v err=%v", result, runner.calls, err)
	}
	encoded, _ := json.Marshal(result)
	for _, want := range []string{"Install it yourself", "start a fresh Codex task", "asdf plugin add", "Install Beads yourself"} {
		if !strings.Contains(string(encoded), want) {
			t.Fatalf("guidance=%s want=%q", encoded, want)
		}
	}
	readyRunner := &fakeRunner{outputs: map[string]Output{
		"codex plugin list --json": {Stdout: `{"installed":[{"pluginId":"godex@godex","installed":true,"enabled":true,"marketplaceSource":{"source":"parmcoder/godex"}}],"available":[]}`},
		"asdf plugin list":         {Stdout: "golang\n"},
		"asdf list golang":         {Stdout: "1.26.5\n"},
	}, errors: map[string]error{}}
	rechecked, err := Inspector{Lookup: available, Runner: readyRunner}.Check(context.Background(), requirements)
	if err != nil || !rechecked.Ready() {
		t.Fatalf("re-check result=%#v err=%v", rechecked, err)
	}
	for _, call := range readyRunner.calls {
		for _, arg := range append([]string{call.name}, call.args...) {
			if arg == "install" || arg == "add" || arg == "plugin-add" {
				t.Fatalf("inspection invoked installer argv: %#v", call)
			}
		}
	}
}

func TestCheckMissingToolGuidanceUsesOfficialURLsWithoutExecutingThem(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("not found") }
	runner := &fakeRunner{outputs: map[string]Output{}, errors: map[string]error{}}
	result, err := Inspector{Lookup: missing, Runner: runner}.Check(context.Background(), Requirements{
		Plugins:  []Plugin{{Source: "parmcoder/godex", Selector: "godex@godex"}},
		Runtimes: []Runtime{{Plugin: "golang", Version: "1.26.5"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, url := range map[string]string{
		"codex": "https://developers.openai.com/codex/cli/",
		"asdf":  "https://asdf-vm.com/guide/getting-started.html",
		"bd":    "https://github.com/gastownhall/beads#installation",
	} {
		finding, ok := findFinding(result, id)
		if !ok || finding.Status != StatusMissing || !strings.Contains(finding.Guidance, url) {
			t.Fatalf("finding %s=%#v want official URL %s", id, finding, url)
		}
	}
	if len(runner.calls) != 0 {
		t.Fatalf("missing tools must not invoke guidance: %#v", runner.calls)
	}
}

func TestCheckReportsMalformedCodexJSONWithoutRawOutput(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]Output{"codex plugin list --json": {Stdout: "[secret-token"}}, errors: map[string]error{}}
	result, err := Inspector{Lookup: available, Runner: runner}.Check(context.Background(), Requirements{Plugins: []Plugin{{Source: "parmcoder/godex", Selector: "godex@godex"}}})
	if err != nil || len(result.Findings) != 4 || result.Findings[0].Status != StatusMalformed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "secret-token") {
		t.Fatalf("result leaks raw output: %s", encoded)
	}
}

func TestCheckRequiresInstalledAndEnabledCodexPlugin(t *testing.T) {
	for name, output := range map[string]string{
		"available only":     `{"installed":[],"available":[{"pluginId":"godex@godex","installed":true,"enabled":true,"marketplaceSource":{"source":"parmcoder/godex"}}]}`,
		"installed disabled": `{"installed":[{"pluginId":"godex@godex","installed":true,"enabled":false,"marketplaceSource":{"source":"parmcoder/godex"}}],"available":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			runner := &fakeRunner{outputs: map[string]Output{"codex plugin list --json": {Stdout: output}}, errors: map[string]error{}}
			result, err := Inspector{Lookup: available, Runner: runner}.Check(context.Background(), Requirements{Plugins: []Plugin{{Source: "parmcoder/godex", Selector: "godex@godex"}}})
			if err != nil || result.Findings[0].Status != StatusReady || result.Findings[1].Status != StatusMissing {
				t.Fatalf("result=%#v err=%v", result, err)
			}
		})
	}
}

func TestCheckRequiresMatchingMarketplaceSource(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]Output{
		"codex plugin list --json": {Stdout: `{"installed":[{"pluginId":"godex@godex","installed":true,"enabled":true,"marketplaceSource":{"source":"https://github.com/other/godex.git"}}],"available":[]}`},
	}, errors: map[string]error{}}
	result, err := Inspector{Lookup: available, Runner: runner}.Check(context.Background(), Requirements{Plugins: []Plugin{{Source: "parmcoder/godex", Selector: "godex@godex"}}})
	if err != nil || result.Findings[0].Status != StatusReady || result.Findings[1].Status != StatusMissing {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestCheckReportsMissingSelectedRuntime(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]Output{
		"codex plugin list --json": {Stdout: `{"installed":[],"available":[]}`},
		"asdf plugin list":         {Stdout: "golang\n"},
		"asdf list golang":         {Stdout: "1.25.0\n"},
	}, errors: map[string]error{}}
	result, err := Inspector{Lookup: available, Runner: runner}.Check(context.Background(), Requirements{Runtimes: []Runtime{{Plugin: "golang", Version: "1.26.5"}}})
	if err != nil || result.Ready() || result.Findings[2].ID != "asdf-runtime:golang@1.26.5" || result.Findings[2].Status != StatusMissing {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func TestCheckHonorsCancellationAndRedactsRunnerFailure(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &fakeRunner{outputs: map[string]Output{}, errors: map[string]error{}}
	if _, err := (Inspector{Lookup: available, Runner: runner}).Check(cancelled, Requirements{Plugins: []Plugin{{Selector: "godex@godex"}}}); !errors.Is(err, context.Canceled) || len(runner.calls) != 0 {
		t.Fatalf("cancelled check err=%v calls=%#v", err, runner.calls)
	}
	secret := "runner-secret-must-not-appear"
	runner = &fakeRunner{outputs: map[string]Output{"codex plugin list --json": {Stdout: secret, Stderr: secret}}, errors: map[string]error{"codex plugin list --json": errors.New(secret)}}
	result, err := (Inspector{Lookup: available, Runner: runner}).Check(context.Background(), Requirements{Plugins: []Plugin{{Source: "parmcoder/godex", Selector: "godex@godex"}}})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), secret) || result.Findings[0].Status != StatusMissing {
		t.Fatalf("result=%s", encoded)
	}
}

func TestInitBeadsUsesOnlyNoninteractiveInitializationArguments(t *testing.T) {
	runner := &fakeRunner{outputs: map[string]Output{}, errors: map[string]error{}}
	inspector := Inspector{Lookup: available, Runner: runner}
	if err := inspector.InitBeads(context.Background(), "/workspace", "platform"); err != nil {
		t.Fatal(err)
	}
	want := []processCall{{dir: "/workspace", name: "bd", args: []string{"init", "--non-interactive", "--init-if-missing", "--quiet", "--skip-agents", "--skip-hooks", "--prefix", "platform"}}}
	if !reflect.DeepEqual(runner.calls, want) {
		t.Fatalf("calls=%#v want=%#v", runner.calls, want)
	}
}

func available(name string) (string, error) { return "/usr/bin/" + name, nil }

func commandKey(name string, args ...string) string {
	return strings.TrimSpace(name + " " + strings.Join(args, " "))
}

func findFinding(result Result, id string) (Finding, bool) {
	for _, finding := range result.Findings {
		if finding.ID == id {
			return finding, true
		}
	}
	return Finding{}, false
}

func assertInspectionOnly(t *testing.T, calls []processCall) {
	t.Helper()
	for _, call := range calls {
		for _, arg := range append([]string{call.name}, call.args...) {
			if strings.Contains(arg, "https://") || arg == "install" || arg == "add" || arg == "marketplace" {
				t.Fatalf("runner received guidance or installer token: %#v", call)
			}
		}
	}
}
