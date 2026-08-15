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
	"path/filepath"
	"regexp"
	"strings"
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
	ReviewState        string   `json:"-"`
}
type QueueResult struct{ FeatureID, ReviewID, Recovery string }
type Recovery struct{ ReviewID, BugID, Recovery string }
type ReleaseReadiness struct {
	Ready    bool
	Blocking []Issue
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
func has(x Issue, label string) bool {
	for _, v := range x.Labels {
		if v == label {
			return true
		}
	}
	return false
}
func review(x Issue) bool       { return has(x, "human-review") && has(x, "e2e") }
func closed(x Issue) bool       { return x.Status == "closed" }
func active(x Issue) bool       { return !closed(x) }
func activeLookup(x Issue) bool { return x.Status == "open" || x.Status == "in_progress" }
func state(x Issue) string {
	for _, v := range []string{"queued", "failed", "retest-queued"} {
		if has(x, v) {
			return v
		}
	}
	return ""
}
func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
func reviewState(labels []string, want string) []string {
	out := make([]string, 0, len(labels)+3)
	for _, label := range labels {
		if label != "queued" && label != "failed" && label != "retest-queued" {
			out = append(out, label)
		}
	}
	if !contains(out, "human-review") {
		out = append(out, "human-review")
	}
	if !contains(out, "e2e") {
		out = append(out, "e2e")
	}
	return append(out, want)
}
func labelsArg(labels []string) string { return strings.Join(labels, ",") }
func (s *Service) addEdge(ctx context.Context, first, second string) error {
	_, err := s.client.run(ctx, "dep", "add", first, second, "--type", "blocks")
	return err
}
func edgeRecovery(first, second string) string {
	return "run: bd dep add " + first + " " + second + " --type blocks"
}
func queueRecovery(feature string) string {
	return "inspect open human E2E reviews for feature " + feature + " then retry QueueReview"
}
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
	raw, err = s.client.run(ctx, "create", "Prepared workspace", "--type", "task", "--priority", "2", "--status", "open")
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
	raw, err := s.client.run(ctx, "create", "Prepared workspace", "--type", "task", "--priority", "2", "--status", "open")
	if err != nil {
		return "", err
	}
	created, err := decode(raw)
	if err != nil || len(created) != 1 || !validID(created[0].ID) {
		return "", errors.New("beads create: invalid_json")
	}
	return created[0].ID, nil
}
func (s *Service) ReadyWork(ctx context.Context) ([]Issue, error) {
	raw, err := s.client.run(ctx, "list", "--ready")
	if err != nil {
		return nil, err
	}
	xs, err := decode(raw)
	if err != nil {
		return nil, err
	}
	out := xs[:0]
	for _, x := range xs {
		if !review(x) && !has(x, "review-bug") {
			out = append(out, x)
		}
	}
	return out, nil
}
func safePath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" || filepath.IsAbs(p) {
		return false
	}
	p = filepath.Clean(p)
	return p != "." && p != ".." && !strings.HasPrefix(p, ".."+string(filepath.Separator))
}
func (s *Service) QueueReview(ctx context.Context, feature, handoff, evidence string) (QueueResult, error) {
	if !validID(feature) || !safePath(handoff) || !safePath(evidence) {
		return QueueResult{}, errors.New("feature ID, workspace-relative handoff path, and evidence path are required")
	}
	f, err := s.show(ctx, feature)
	if err != nil {
		return QueueResult{}, err
	}
	if closed(f) || (f.Type != "feature" && f.Type != "task") {
		return QueueResult{}, errors.New("feature must be an open feature or task")
	}
	raw, err := s.client.run(ctx, "list", "--parent", feature, "--status", "open,in_progress")
	if err != nil {
		return QueueResult{}, err
	}
	xs, err := decode(raw)
	if err != nil {
		return QueueResult{}, err
	}
	var existing []Issue
	for _, x := range xs {
		if review(x) {
			existing = append(existing, x)
		}
	}
	if len(existing) > 1 {
		return QueueResult{}, errors.New("multiple open human E2E reviews found")
	}
	if len(existing) == 1 {
		if err := s.addEdge(ctx, feature, existing[0].ID); err != nil {
			return QueueResult{FeatureID: feature, ReviewID: existing[0].ID, Recovery: edgeRecovery(feature, existing[0].ID)}, err
		}
		return QueueResult{FeatureID: feature, ReviewID: existing[0].ID}, nil
	}
	raw, err = s.client.run(ctx, "create", "Human E2E review for "+feature, "--type", "task", "--parent", feature, "--labels", "human-review,e2e,queued", "--description", "handoff: "+handoff+"\nevidence: "+evidence)
	if err != nil {
		return QueueResult{FeatureID: feature, Recovery: queueRecovery(feature)}, err
	}
	xs, err = decode(raw)
	if err != nil || len(xs) != 1 {
		return QueueResult{FeatureID: feature, Recovery: queueRecovery(feature)}, errors.New("invalid review creation response")
	}
	if err := s.addEdge(ctx, feature, xs[0].ID); err != nil {
		return QueueResult{FeatureID: feature, ReviewID: xs[0].ID, Recovery: edgeRecovery(feature, xs[0].ID)}, err
	}
	return QueueResult{FeatureID: feature, ReviewID: xs[0].ID}, nil
}
func (s *Service) ListReviews(ctx context.Context) ([]Issue, error) {
	raw, err := s.client.run(ctx, "list", "--label", "human-review", "--label", "e2e", "--status", "open,in_progress")
	if err != nil {
		return nil, err
	}
	xs, err := decode(raw)
	if err != nil {
		return nil, err
	}
	for i := range xs {
		xs[i].ReviewState = state(xs[i])
	}
	return xs, nil
}
func (s *Service) ReleaseReadiness(ctx context.Context) (ReleaseReadiness, error) {
	raw, err := s.client.run(ctx, "list", "--status", "open,in_progress,blocked,deferred")
	if err != nil {
		return ReleaseReadiness{}, err
	}
	xs, err := decode(raw)
	if err != nil {
		return ReleaseReadiness{}, err
	}
	var b []Issue
	for _, x := range xs {
		if review(x) || has(x, "review-bug") {
			b = append(b, x)
		}
	}
	return ReleaseReadiness{len(b) == 0, b}, nil
}
func (s *Service) RequeueAfterFix(ctx context.Context, id string) (Recovery, error) {
	if !validID(id) {
		return Recovery{}, errors.New("invalid review ID")
	}
	x, err := s.show(ctx, id)
	if err != nil {
		return Recovery{}, err
	}
	if !review(x) || x.Status != "open" || state(x) != "failed" {
		return Recovery{}, errors.New("review must be an open failed human E2E review")
	}
	raw, err := s.client.run(ctx, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id="+id)
	if err != nil {
		return Recovery{}, err
	}
	bugs, err := decode(raw)
	if err != nil {
		return Recovery{}, err
	}
	if len(bugs) == 0 {
		return Recovery{ReviewID: id, Recovery: "create or link a review bug before requeue"}, errors.New("review has no linked review bug")
	}
	for _, bug := range bugs {
		if active(bug) {
			return Recovery{ReviewID: id, BugID: bug.ID, Recovery: "close linked review bug before requeue"}, errors.New("linked review bug remains active")
		}
	}
	labels := labelsArg(reviewState(x.Labels, "retest-queued"))
	_, err = s.client.run(ctx, "update", id, "--status", "open", "--set-labels", labels)
	if err != nil {
		return Recovery{ReviewID: id, Recovery: "rerun: bd update " + id + " --status open --set-labels " + labels}, err
	}
	return Recovery{ReviewID: id}, nil
}
