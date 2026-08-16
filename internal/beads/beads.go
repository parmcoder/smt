// Package beads provides the small safe review lifecycle SMT exposes.
package beads

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"time"
)

type ProcessRunner interface {
	Run(context.Context, string, string, ...string) (ProcessResult, error)
}

// ProcessResult contains non-sensitive process telemetry and separated output.
type ProcessResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Duration time.Duration
}
type CommandRunner struct{}

func (CommandRunner) Run(ctx context.Context, dir, name string, args ...string) (ProcessResult, error) {
	started := time.Now()
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	var stdout, stderr bytes.Buffer
	c.Stdout, c.Stderr = &stdout, &stderr
	err := c.Run()
	result := ProcessResult{Stdout: stdout.String(), Stderr: stderr.String(), Duration: time.Since(started)}
	if c.ProcessState != nil {
		result.ExitCode = c.ProcessState.ExitCode()
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, err
}

type Issue struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Design             string   `json:"design"`
	AcceptanceCriteria string   `json:"acceptance_criteria"`
	ExternalRef        string   `json:"external_ref"`
	Priority           int      `json:"priority"`
	Status             string   `json:"status"`
	Type               string   `json:"issue_type"`
	Labels             []string `json:"labels"`
	Parent             string   `json:"parent"`
	Dependencies       []Issue  `json:"dependencies"`
	DependencyType     string   `json:"dependency_type"`
}
type Client struct {
	Dir     string
	Runner  ProcessRunner
	Verbose io.Writer
}
type Service struct{ client Client }

func New(dir string, runner ProcessRunner) *Service {
	return &Service{client: Client{Dir: dir, Runner: runner}}
}

// NewWithVerbose constructs a service with safe operation telemetry output.
func NewWithVerbose(dir string, runner ProcessRunner, verbose io.Writer) *Service {
	service := New(dir, runner)
	service.client.Verbose = verbose
	return service
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validID(s string) bool { return idPattern.MatchString(s) }
func (c Client) run(ctx context.Context, args ...string) (string, error) {
	if c.Runner == nil {
		return "", errors.New("beads operation unavailable: unavailable")
	}
	operation := args[0]
	result, err := c.Runner.Run(ctx, c.Dir, "bd", append(args, "--json")...)
	classification := ""
	if err != nil || result.ExitCode != 0 {
		classification = "command_failed"
		if operation == "show" && result.ExitCode == 1 {
			classification = "not_found"
		}
		if result.ExitCode == 0 && err != nil {
			classification = "unavailable"
		}
		if ctx.Err() != nil {
			classification = "unavailable"
		}
		c.log(operation, classification, result)
		return "", fmt.Errorf("beads %s: %s", operation, classification)
	}
	c.log(operation, classification, result)
	return result.Stdout, nil
}
func (c Client) log(operation, classification string, result ProcessResult) {
	if c.Verbose == nil {
		return
	}
	if classification == "" {
		classification = "ok"
	}
	fmt.Fprintf(c.Verbose, "operation=%s classification=%s exit_code=%d duration_ms=%d stdout_bytes=%d stderr_bytes=%d\n", operation, classification, result.ExitCode, result.Duration.Milliseconds(), len(result.Stdout), len(result.Stderr))
}
func decode(raw string) ([]Issue, error) {
	var xs []Issue
	if err := json.Unmarshal([]byte(raw), &xs); err == nil {
		return checked(xs)
	}
	var x Issue
	if err := json.Unmarshal([]byte(raw), &x); err != nil || x.ID == "" {
		return nil, errors.New("invalid_json")
	}
	return checked([]Issue{x})
}
func checked(xs []Issue) ([]Issue, error) {
	for _, x := range xs {
		if !validID(x.ID) {
			return nil, errors.New("invalid_json")
		}
	}
	return xs, nil
}
func activeLookup(x Issue) bool { return x.Status == "open" || x.Status == "in_progress" }
func (s *Service) show(ctx context.Context, id string) (Issue, error) {
	if !validID(id) {
		return Issue{}, errors.New("invalid issue ID")
	}
	raw, err := s.client.run(ctx, "show", id)
	if err != nil {
		return Issue{}, err
	}
	xs, err := decode(raw)
	if err != nil || len(xs) != 1 {
		return Issue{}, errors.New("beads show: invalid_json")
	}
	if !activeLookup(xs[0]) {
		return Issue{}, fmt.Errorf("beads show %s: not_found", id)
	}
	return xs[0], nil
}

// ShowIssue returns one safe issue record for read-only workspace planning.
func (s *Service) ShowIssue(ctx context.Context, id string) (Issue, error) {
	return s.show(ctx, id)
}

// ListOpenChildren returns direct open children without changing Beads state.
func (s *Service) ListOpenChildren(ctx context.Context, parent string) ([]Issue, error) {
	if !validID(parent) {
		return nil, errors.New("invalid feature ID")
	}
	raw, err := s.client.run(ctx, "list", "--parent", parent, "--status", "open,in_progress")
	if err != nil {
		return nil, err
	}
	xs, err := decode(raw)
	if err != nil {
		return nil, err
	}
	activeChildren := xs[:0]
	for _, x := range xs {
		if x.Parent == parent && activeLookup(x) {
			activeChildren = append(activeChildren, x)
		}
	}
	return activeChildren, nil
}

// EnsurePreparedWorkspaceTask returns the sole active P2 task named
// Prepared workspace, creating it when absent.
func (s *Service) EnsurePreparedWorkspaceTask(ctx context.Context) (string, error) {
	raw, err := s.client.run(ctx, "list", "--status", "open,in_progress")
	if err != nil {
		return "", err
	}
	issues, err := decode(raw)
	if err != nil {
		return "", errors.New("beads list: invalid_json")
	}
	var matches []Issue
	for _, issue := range issues {
		if activeLookup(issue) && issue.Title == "Prepared workspace" && issue.Priority == 2 {
			matches = append(matches, issue)
		}
	}
	if len(matches) > 1 {
		return "", errors.New("beads prepared workspace: command_failed")
	}
	if len(matches) == 1 {
		return matches[0].ID, nil
	}
	raw, err = s.client.run(ctx, "create", "Prepared workspace", "--type", "task", "--priority", "2")
	if err != nil {
		return "", err
	}
	created, err := decode(raw)
	if err != nil || len(created) != 1 || !validID(created[0].ID) {
		return "", errors.New("beads create: invalid_json")
	}
	return created[0].ID, nil
}

// CreatePreparedWorkspaceTask is the explicit lifecycle name for callers that
// need the singleton prepared-workspace task ID.
func (s *Service) CreatePreparedWorkspaceTask(ctx context.Context) (string, error) {
	raw, err := s.client.run(ctx, "create", "Prepared workspace", "--type", "task", "--priority", "2")
	if err != nil {
		return "", err
	}
	created, err := decode(raw)
	if err != nil || len(created) != 1 || !validID(created[0].ID) {
		return "", errors.New("beads create: invalid_json")
	}
	return created[0].ID, nil
}
