// Package beads provides the small, human-review lifecycle SMT needs from bd.
package beads

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ProcessRunner permits deterministic tests without invoking bd.
type ProcessRunner interface {
	Run(context.Context, string, string, ...string) (string, error)
}

// CommandRunner executes a command with an explicit working directory.
type CommandRunner struct{}

func (CommandRunner) Run(ctx context.Context, dir, name string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir
	output, err := command.Output()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return string(output), err
}

type Client struct {
	Dir    string
	Runner ProcessRunner
}

type Issue struct {
	ID             string          `json:"id"`
	Title          string          `json:"title"`
	Status         string          `json:"status"`
	Type           string          `json:"issue_type"`
	Labels         []string        `json:"labels"`
	Parent         string          `json:"parent"`
	Metadata       json.RawMessage `json:"metadata"`
	Dependencies   []Issue         `json:"dependencies"`
	DependencyType string          `json:"dependency_type"`
	ReviewState    string          `json:"-"`
}

type QueueResult struct {
	FeatureID string
	ReviewID  string
	Recovery  string
}

type ReleaseReadiness struct {
	Ready    bool
	Blocking []Issue
}

type FailureInput struct {
	Title, Reproduction, Expected, Actual, Evidence string
}

type Recovery struct {
	ReviewID string
	BugID    string
	Recovery string
}

type Service struct{ client Client }

func New(dir string, runner ProcessRunner) *Service {
	return &Service{client: Client{Dir: dir, Runner: runner}}
}

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func validID(id string) bool { return idPattern.MatchString(id) }

func (c Client) run(ctx context.Context, args ...string) (string, error) {
	if c.Runner == nil {
		return "", errors.New("Beads runner is required")
	}
	output, err := c.Runner.Run(ctx, c.Dir, "bd", append(args, "--json")...)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		// bd may include issue text, paths, or environment details in stderr. Do
		// not leak it through the CLI error contract.
		return "", errors.New("Beads command failed")
	}
	return output, nil
}

func decodeIssues(raw string) ([]Issue, error) {
	var issues []Issue
	if err := json.Unmarshal([]byte(raw), &issues); err == nil {
		return checkedIssues(issues)
	}
	var issue Issue
	if err := json.Unmarshal([]byte(raw), &issue); err != nil || issue.ID == "" {
		return nil, errors.New("malformed Beads JSON")
	}
	return checkedIssues([]Issue{issue})
}

func checkedIssues(issues []Issue) ([]Issue, error) {
	for _, issue := range issues {
		if !validID(issue.ID) {
			return nil, errors.New("invalid Beads issue response")
		}
	}
	return issues, nil
}

func (s *Service) show(ctx context.Context, id string) (Issue, error) {
	if !validID(id) {
		return Issue{}, errors.New("invalid issue ID")
	}
	raw, err := s.client.run(ctx, "show", id)
	if err != nil {
		return Issue{}, err
	}
	issues, err := decodeIssues(raw)
	if err != nil || len(issues) != 1 {
		return Issue{}, errors.New("invalid Beads issue response")
	}
	return issues[0], nil
}

func has(issue Issue, label string) bool {
	for _, value := range issue.Labels {
		if value == label {
			return true
		}
	}
	return false
}

func review(issue Issue) bool { return has(issue, "human-review") && has(issue, "e2e") }
func closed(issue Issue) bool { return issue.Status == "closed" }
func active(issue Issue) bool { return !closed(issue) }

func stateOf(issue Issue) string {
	for _, state := range []string{"queued", "failed", "retest-queued"} {
		if has(issue, state) {
			return state
		}
	}
	return ""
}

func metadataValue(issue Issue, key string) string {
	var metadata map[string]string
	if json.Unmarshal(issue.Metadata, &metadata) != nil {
		return ""
	}
	return metadata[key]
}

func safePath(path string) bool {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || filepath.IsAbs(trimmed) {
		return false
	}
	clean := filepath.Clean(trimmed)
	return clean != "." && clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}

func reviewState(labels []string, state string) []string {
	result := make([]string, 0, len(labels)+3)
	for _, label := range labels {
		if label != "queued" && label != "failed" && label != "retest-queued" {
			result = append(result, label)
		}
	}
	if !contains(result, "human-review") {
		result = append(result, "human-review")
	}
	if !contains(result, "e2e") {
		result = append(result, "e2e")
	}
	return append(result, state)
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func labelsArg(labels []string) string { return strings.Join(labels, ",") }

func (s *Service) addEdge(ctx context.Context, first, second string) error {
	_, err := s.client.run(ctx, "dep", "add", first, second, "--type", "blocks")
	return err
}

func (s *Service) ReadyWork(ctx context.Context) ([]Issue, error) {
	raw, err := s.client.run(ctx, "list", "--ready")
	if err != nil {
		return nil, err
	}
	issues, err := decodeIssues(raw)
	if err != nil {
		return nil, err
	}
	result := issues[:0]
	for _, issue := range issues {
		if !review(issue) && !has(issue, "review-bug") {
			result = append(result, issue)
		}
	}
	return result, nil
}

func (s *Service) QueueReview(ctx context.Context, featureID, handoff, evidence string) (QueueResult, error) {
	if !validID(featureID) || !safePath(handoff) || !safePath(evidence) {
		return QueueResult{}, errors.New("feature ID, workspace-relative handoff path, and evidence path are required")
	}
	feature, err := s.show(ctx, featureID)
	if err != nil {
		return QueueResult{}, err
	}
	if closed(feature) || (feature.Type != "feature" && feature.Type != "task") {
		return QueueResult{}, errors.New("feature must be an open feature or task")
	}
	raw, err := s.client.run(ctx, "list", "--parent", featureID, "--status", "open")
	if err != nil {
		return QueueResult{}, err
	}
	children, err := decodeIssues(raw)
	if err != nil {
		return QueueResult{}, err
	}
	var existing []Issue
	for _, child := range children {
		if review(child) {
			existing = append(existing, child)
		}
	}
	if len(existing) > 1 {
		return QueueResult{}, errors.New("multiple open human E2E reviews found")
	}
	if len(existing) == 1 {
		if err := s.addEdge(ctx, featureID, existing[0].ID); err != nil {
			return QueueResult{FeatureID: featureID, ReviewID: existing[0].ID, Recovery: edgeRecovery(featureID, existing[0].ID)}, err
		}
		return QueueResult{FeatureID: featureID, ReviewID: existing[0].ID}, nil
	}
	description := "handoff: " + handoff + "\nevidence: " + evidence
	raw, err = s.client.run(ctx, "create", "Human E2E review for "+featureID, "--type", "task", "--parent", featureID, "--labels", "human-review,e2e,queued", "--description", description)
	if err != nil {
		return QueueResult{FeatureID: featureID, Recovery: queueRecovery(featureID)}, err
	}
	created, err := decodeIssues(raw)
	if err != nil || len(created) != 1 {
		return QueueResult{FeatureID: featureID, Recovery: queueRecovery(featureID)}, errors.New("invalid review creation response")
	}
	if err := s.addEdge(ctx, featureID, created[0].ID); err != nil {
		return QueueResult{FeatureID: featureID, ReviewID: created[0].ID, Recovery: edgeRecovery(featureID, created[0].ID)}, err
	}
	return QueueResult{FeatureID: featureID, ReviewID: created[0].ID}, nil
}

func edgeRecovery(first, second string) string {
	return "run: bd dep add " + first + " " + second + " --type blocks"
}

func queueRecovery(featureID string) string {
	return "inspect open human E2E reviews for feature " + featureID + " then retry QueueReview"
}

func humanFailRecovery(reviewID string) string {
	return "inspect linked review bugs for review " + reviewID + " then retry HumanFail"
}

func (s *Service) ListReviews(ctx context.Context) ([]Issue, error) {
	raw, err := s.client.run(ctx, "list", "--label", "human-review", "--label", "e2e", "--status", "open")
	if err != nil {
		return nil, err
	}
	issues, err := decodeIssues(raw)
	if err != nil {
		return nil, err
	}
	for index := range issues {
		issues[index].ReviewState = stateOf(issues[index])
	}
	return issues, nil
}

func (s *Service) ReleaseReadiness(ctx context.Context) (ReleaseReadiness, error) {
	raw, err := s.client.run(ctx, "list", "--status", "open,in_progress,blocked,deferred")
	if err != nil {
		return ReleaseReadiness{}, err
	}
	issues, err := decodeIssues(raw)
	if err != nil {
		return ReleaseReadiness{}, err
	}
	var blocking []Issue
	for _, issue := range issues {
		if review(issue) || has(issue, "review-bug") {
			blocking = append(blocking, issue)
		}
	}
	return ReleaseReadiness{Ready: len(blocking) == 0, Blocking: blocking}, nil
}

func required(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

func (s *Service) linkedReviewBugs(ctx context.Context, reviewID string) ([]Issue, error) {
	raw, err := s.client.run(ctx, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id="+reviewID)
	if err != nil {
		return nil, err
	}
	issues, err := decodeIssues(raw)
	if err != nil {
		return nil, err
	}
	return issues, nil
}

func activeBugs(issues []Issue) []Issue {
	activeBugs := issues[:0]
	for _, issue := range issues {
		if active(issue) {
			activeBugs = append(activeBugs, issue)
		}
	}
	return activeBugs
}

func hasActiveBlocker(issue Issue) bool {
	for _, dependency := range issue.Dependencies {
		if dependency.DependencyType == "blocks" && active(dependency) {
			return true
		}
	}
	return false
}

func (s *Service) HumanPass(ctx context.Context, reviewID, reviewer, evidence string) (Recovery, error) {
	if !validID(reviewID) || !required(reviewer, evidence) {
		return Recovery{}, errors.New("reviewer and evidence are required")
	}
	reviewIssue, err := s.show(ctx, reviewID)
	if err != nil {
		return Recovery{}, err
	}
	if !review(reviewIssue) || reviewIssue.Status != "open" || (stateOf(reviewIssue) != "queued" && stateOf(reviewIssue) != "retest-queued") {
		return Recovery{}, errors.New("review must be an open queued or retest-queued human E2E review")
	}
	if hasActiveBlocker(reviewIssue) {
		return Recovery{ReviewID: reviewID}, errors.New("review has an active blocker")
	}
	bugs, err := s.linkedReviewBugs(ctx, reviewID)
	if err != nil {
		return Recovery{ReviewID: reviewID}, err
	}
	if len(activeBugs(bugs)) > 0 {
		return Recovery{ReviewID: reviewID}, errors.New("linked review bug remains active")
	}
	if _, err := s.client.run(ctx, "update", reviewID, "--append-notes", "reviewer: "+reviewer+"\nevidence: "+evidence); err != nil {
		return Recovery{ReviewID: reviewID, Recovery: "rerun: bd update " + reviewID + " --append-notes <human review evidence>"}, err
	}
	_, err = s.client.run(ctx, "close", reviewID, "--reason", "human E2E passed")
	if err != nil {
		return Recovery{ReviewID: reviewID, Recovery: "run: bd close " + reviewID + " --reason human E2E passed"}, err
	}
	return Recovery{ReviewID: reviewID}, nil
}

func (s *Service) HumanFail(ctx context.Context, reviewID string, input FailureInput) (Recovery, error) {
	if !validID(reviewID) || !required(input.Title, input.Reproduction, input.Expected, input.Actual, input.Evidence) {
		return Recovery{}, errors.New("failure title, reproduction, expected, actual, and evidence are required")
	}
	reviewIssue, err := s.show(ctx, reviewID)
	if err != nil {
		return Recovery{}, err
	}
	if !review(reviewIssue) || closed(reviewIssue) {
		return Recovery{}, errors.New("review must be open human E2E review")
	}
	if !validID(reviewIssue.Parent) {
		return Recovery{ReviewID: reviewID}, errors.New("review must have a valid feature parent")
	}
	parent, err := s.show(ctx, reviewIssue.Parent)
	if err != nil {
		return Recovery{ReviewID: reviewID}, err
	}
	if parent.Type != "feature" || closed(parent) {
		return Recovery{ReviewID: reviewID}, errors.New("review parent must be an open feature")
	}
	// A human failure is durable even if a later bug/dependency write fails.
	if _, err := s.client.run(ctx, "update", reviewID, "--set-labels", labelsArg(reviewState(reviewIssue.Labels, "failed"))); err != nil {
		return Recovery{ReviewID: reviewID, Recovery: humanFailRecovery(reviewID)}, err
	}
	bugs, err := s.linkedReviewBugs(ctx, reviewID)
	if err != nil {
		return Recovery{ReviewID: reviewID, Recovery: humanFailRecovery(reviewID)}, err
	}
	if active := activeBugs(bugs); len(active) > 1 {
		return Recovery{ReviewID: reviewID, Recovery: humanFailRecovery(reviewID)}, errors.New("multiple active linked review bugs found")
	} else if len(active) == 1 {
		bug := active[0]
		if err := s.addEdge(ctx, reviewID, bug.ID); err != nil {
			return Recovery{ReviewID: reviewID, BugID: bug.ID, Recovery: edgeRecovery(reviewID, bug.ID)}, err
		}
		return Recovery{ReviewID: reviewID, BugID: bug.ID}, nil
	}
	description := "reproduction: " + input.Reproduction + "\nexpected: " + input.Expected + "\nactual: " + input.Actual + "\nevidence: " + input.Evidence
	raw, err := s.client.run(ctx, "create", input.Title, "--type", "bug", "--parent", reviewIssue.Parent, "--no-inherit-labels", "--labels", "review-bug", "--metadata", `{"review_id":"`+reviewID+`"}`, "--deps", "discovered-from:"+reviewID, "--description", description)
	if err != nil {
		return Recovery{ReviewID: reviewID, Recovery: humanFailRecovery(reviewID)}, err
	}
	created, err := decodeIssues(raw)
	if err != nil || len(created) != 1 {
		return Recovery{ReviewID: reviewID, Recovery: humanFailRecovery(reviewID)}, errors.New("invalid bug creation response")
	}
	if err := s.addEdge(ctx, reviewID, created[0].ID); err != nil {
		return Recovery{ReviewID: reviewID, BugID: created[0].ID, Recovery: edgeRecovery(reviewID, created[0].ID)}, err
	}
	return Recovery{ReviewID: reviewID, BugID: created[0].ID}, nil
}

func (s *Service) RequeueAfterFix(ctx context.Context, reviewID string) (Recovery, error) {
	if !validID(reviewID) {
		return Recovery{}, errors.New("invalid review ID")
	}
	reviewIssue, err := s.show(ctx, reviewID)
	if err != nil {
		return Recovery{}, err
	}
	if !review(reviewIssue) || reviewIssue.Status != "open" || stateOf(reviewIssue) != "failed" {
		return Recovery{}, errors.New("review must be an open failed human E2E review")
	}
	bugs, err := s.linkedReviewBugs(ctx, reviewID)
	if err != nil {
		return Recovery{}, err
	}
	if len(bugs) == 0 {
		return Recovery{ReviewID: reviewID, Recovery: "create or link a review bug before requeue"}, errors.New("review has no linked review bug")
	}
	if active := activeBugs(bugs); len(active) > 0 {
		return Recovery{ReviewID: reviewID, BugID: active[0].ID, Recovery: "close linked review bug before requeue"}, errors.New("linked review bug remains active")
	}
	labels := labelsArg(reviewState(reviewIssue.Labels, "retest-queued"))
	if _, err := s.client.run(ctx, "update", reviewID, "--status", "open", "--set-labels", labels); err != nil {
		return Recovery{ReviewID: reviewID, Recovery: "rerun: bd update " + reviewID + " --status open --set-labels " + labels}, err
	}
	return Recovery{ReviewID: reviewID}, nil
}
