// Package prereq observes the local tools required to create a Codex-assisted
// workspace. It never installs, downloads, or changes those tools.
package prereq

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Plugin is the configured source and selector for one required Codex plugin.
type Plugin struct {
	Source   string
	Selector string
}

// Runtime is one asdf plugin and pinned runtime version.
type Runtime struct {
	Plugin  string
	Version string
}

// Requirements is the selected prerequisite set for one workspace setup.
type Requirements struct {
	Plugins  []Plugin
	Runtimes []Runtime
}

// Status describes one prerequisite observation.
type Status string

const (
	StatusReady     Status = "ready"
	StatusMissing   Status = "missing"
	StatusMalformed Status = "malformed"
)

// Finding is one stable, safe-to-display prerequisite result.
type Finding struct {
	ID       string `json:"id"`
	Status   Status `json:"status"`
	Message  string `json:"message"`
	Guidance string `json:"guidance,omitempty"`
}

// Result is the structured output of an observation-only prerequisite check.
type Result struct {
	Findings []Finding `json:"findings"`
}

// Ready reports whether every finding passed.
func (r Result) Ready() bool {
	for _, finding := range r.Findings {
		if finding.Status != StatusReady {
			return false
		}
	}
	return true
}

// Lookup reports whether a command is available on PATH.
type Lookup func(string) (string, error)

// Output retains command output inside the adapter. Callers must not display
// it because tool output can contain unrelated local data.
type Output struct {
	Stdout string
	Stderr string
}

// ProcessRunner executes an argument-array command in an optional working directory.
type ProcessRunner interface {
	Run(context.Context, string, string, ...string) (Output, error)
}

// CommandRunner invokes local commands without a shell.
type CommandRunner struct{}

// Run invokes name and args using exec.CommandContext. It intentionally does
// not interpret shell syntax or surface raw command output in errors.
func (CommandRunner) Run(ctx context.Context, dir, name string, args ...string) (Output, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	stdout, err := command.Output()
	if err == nil {
		return Output{Stdout: string(stdout)}, nil
	}
	if ctx.Err() != nil {
		return Output{}, ctx.Err()
	}
	return Output{}, err
}

// Inspector observes prerequisites through injected process and PATH adapters.
type Inspector struct {
	Lookup Lookup
	Runner ProcessRunner
}

// New creates an Inspector backed by the local process environment.
func New() Inspector { return Inspector{Lookup: exec.LookPath, Runner: CommandRunner{}} }

// Check inspects Codex plugin state, selected asdf runtimes, and Beads
// availability. It never executes installer, package-manager, or remote-script
// commands. A human must act on any guidance and re-check afterward.
func (i Inspector) Check(ctx context.Context, requirements Requirements) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	result := Result{Findings: make([]Finding, 0, len(requirements.Plugins)+len(requirements.Runtimes)+3)}
	result.Findings = append(result.Findings, i.codex(ctx, requirements.Plugins)...)
	result.Findings = append(result.Findings, i.asdf(ctx, requirements.Runtimes)...)
	result.Findings = append(result.Findings, i.beads()...)
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return result, nil
}

// InitBeads exposes the noninteractive Beads initialization invocation for the
// later atomic scaffold step. Check does not call it.
func (i Inspector) InitBeads(ctx context.Context, dir, prefix string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := i.command(ctx, "bd", dir, "init", "--non-interactive", "--init-if-missing", "--quiet", "--skip-agents", "--skip-hooks", "--prefix", prefix); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("initialize Beads: command failed")
	}
	return nil
}

func (i Inspector) codex(ctx context.Context, plugins []Plugin) []Finding {
	if !i.available("codex") {
		findings := make([]Finding, 0, len(plugins)+1)
		findings = append(findings, Finding{ID: "codex", Status: StatusMissing, Message: "Codex is not available", Guidance: codexInstallGuidance()})
		for _, plugin := range plugins {
			findings = append(findings, missingPlugin(plugin, "Codex is not available"))
		}
		return findings
	}
	if len(plugins) == 0 {
		return []Finding{{ID: "codex", Status: StatusReady, Message: "Codex is available"}}
	}
	output, err := i.command(ctx, "codex", "", "plugin", "list", "--json")
	if err != nil {
		findings := make([]Finding, 0, len(plugins)+1)
		findings = append(findings, Finding{ID: "codex", Status: StatusMissing, Message: "Codex plugin state could not be inspected", Guidance: recheckGuidance()})
		for _, plugin := range plugins {
			findings = append(findings, missingPlugin(plugin, "Codex plugin state could not be inspected"))
		}
		return findings
	}
	installed, err := installedPluginsFromJSON(output.Stdout)
	if err != nil {
		findings := make([]Finding, 0, len(plugins)+1)
		findings = append(findings, Finding{ID: "codex", Status: StatusMalformed, Message: "Codex returned malformed plugin JSON", Guidance: recheckGuidance()})
		for _, plugin := range plugins {
			findings = append(findings, Finding{ID: "codex-plugin:" + plugin.Selector, Status: StatusMalformed, Message: "Codex returned malformed plugin JSON", Guidance: recheckGuidance()})
		}
		return findings
	}
	findings := make([]Finding, 0, len(plugins)+1)
	findings = append(findings, Finding{ID: "codex", Status: StatusReady, Message: "Codex is available"})
	for _, plugin := range plugins {
		if installed[Plugin{Source: normalizeGitHubSource(plugin.Source), Selector: plugin.Selector}] {
			findings = append(findings, Finding{ID: "codex-plugin:" + plugin.Selector, Status: StatusReady, Message: "Codex plugin " + plugin.Selector + " is available"})
			continue
		}
		findings = append(findings, missingPlugin(plugin, "Codex plugin "+plugin.Selector+" is not available"))
	}
	return findings
}

func (i Inspector) asdf(ctx context.Context, runtimes []Runtime) []Finding {
	if !i.available("asdf") {
		findings := make([]Finding, 0, len(runtimes)+1)
		findings = append(findings, Finding{ID: "asdf", Status: StatusMissing, Message: "asdf is not available", Guidance: asdfInstallGuidance()})
		for _, runtime := range runtimes {
			findings = append(findings, missingRuntime(runtime, "asdf is not available"))
		}
		return findings
	}
	if len(runtimes) == 0 {
		return []Finding{{ID: "asdf", Status: StatusReady, Message: "asdf is available"}}
	}
	plugins, err := i.command(ctx, "asdf", "", "plugin", "list")
	if err != nil {
		findings := make([]Finding, 0, len(runtimes)+1)
		findings = append(findings, Finding{ID: "asdf", Status: StatusMissing, Message: "asdf plugin state could not be inspected", Guidance: recheckGuidance()})
		for _, runtime := range runtimes {
			findings = append(findings, missingRuntime(runtime, "asdf plugin state could not be inspected"))
		}
		return findings
	}
	installed := lines(plugins.Stdout)
	findings := make([]Finding, 0, len(runtimes)+1)
	findings = append(findings, Finding{ID: "asdf", Status: StatusReady, Message: "asdf is available"})
	for _, runtime := range runtimes {
		if !installed[runtime.Plugin] {
			findings = append(findings, missingRuntime(runtime, "asdf plugin "+runtime.Plugin+" is not available"))
			continue
		}
		versions, err := i.command(ctx, "asdf", "", "list", runtime.Plugin)
		if err != nil {
			findings = append(findings, missingRuntime(runtime, "asdf runtime "+runtime.Plugin+" "+runtime.Version+" could not be inspected"))
			continue
		}
		if lines(versions.Stdout)[runtime.Version] {
			findings = append(findings, Finding{ID: "asdf-runtime:" + runtime.Plugin + "@" + runtime.Version, Status: StatusReady, Message: "asdf runtime " + runtime.Plugin + " " + runtime.Version + " is installed"})
			continue
		}
		findings = append(findings, missingRuntime(runtime, "asdf runtime "+runtime.Plugin+" "+runtime.Version+" is not installed"))
	}
	return findings
}

func (i Inspector) beads() []Finding {
	if i.available("bd") {
		return []Finding{{ID: "bd", Status: StatusReady, Message: "Beads is available"}}
	}
	return []Finding{{ID: "bd", Status: StatusMissing, Message: "Beads is not available", Guidance: beadsInstallGuidance()}}
}

func (i Inspector) available(name string) bool {
	if i.Lookup == nil {
		return false
	}
	_, err := i.Lookup(name)
	return err == nil
}

func (i Inspector) command(ctx context.Context, name, dir string, args ...string) (Output, error) {
	if i.Runner == nil {
		return Output{}, errors.New("process runner is unavailable")
	}
	return i.Runner.Run(ctx, dir, name, args...)
}

func missingPlugin(plugin Plugin, message string) Finding {
	return Finding{ID: "codex-plugin:" + plugin.Selector, Status: StatusMissing, Message: message, Guidance: "Install it yourself with Codex: codex plugin marketplace add https://github.com/" + plugin.Source + "; codex plugin add " + plugin.Selector + ". Then start a fresh Codex task so the skills load. SMT never installs plugins."}
}

func missingRuntime(runtime Runtime, message string) Finding {
	return Finding{ID: "asdf-runtime:" + runtime.Plugin + "@" + runtime.Version, Status: StatusMissing, Message: message, Guidance: "Install it yourself: asdf plugin add " + runtime.Plugin + "; asdf install " + runtime.Plugin + " " + runtime.Version + ". Then re-check prerequisites. SMT never installs runtimes."}
}

func recheckGuidance() string {
	return "Inspect the local tool output, correct the prerequisite yourself, and re-check. SMT never installs prerequisites."
}

func codexInstallGuidance() string {
	return "Install Codex CLI yourself using the official instructions: https://developers.openai.com/codex/cli/ then re-check prerequisites. SMT never installs global tools."
}

func asdfInstallGuidance() string {
	return "Install asdf yourself using the official instructions: https://asdf-vm.com/guide/getting-started.html then re-check prerequisites. SMT never installs global tools."
}

func beadsInstallGuidance() string {
	return "Install Beads yourself using the official instructions: https://github.com/gastownhall/beads#installation then re-check prerequisites. SMT never installs global tools."
}

func lines(output string) map[string]bool {
	result := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if line != "" {
			result[line] = true
		}
	}
	return result
}

func installedPluginsFromJSON(raw string) (map[Plugin]bool, error) {
	var response struct {
		Installed json.RawMessage `json:"installed"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return nil, err
	}
	if len(response.Installed) == 0 {
		return nil, errors.New("installed plugins are absent")
	}
	var installed []struct {
		PluginID          string `json:"pluginId"`
		Installed         bool   `json:"installed"`
		Enabled           bool   `json:"enabled"`
		MarketplaceSource struct {
			Source string `json:"source"`
		} `json:"marketplaceSource"`
	}
	if err := json.Unmarshal(response.Installed, &installed); err != nil {
		return nil, err
	}
	plugins := map[Plugin]bool{}
	for _, plugin := range installed {
		if plugin.PluginID == "" || plugin.MarketplaceSource.Source == "" {
			return nil, errors.New("installed plugin identity is absent")
		}
		if plugin.Installed && plugin.Enabled {
			plugins[Plugin{Source: normalizeGitHubSource(plugin.MarketplaceSource.Source), Selector: plugin.PluginID}] = true
		}
	}
	return plugins, nil
}

func normalizeGitHubSource(source string) string {
	const githubPrefix = "https://github.com/"
	if !strings.HasPrefix(source, githubPrefix) {
		return source
	}
	return strings.TrimSuffix(strings.TrimPrefix(source, githubPrefix), ".git")
}
