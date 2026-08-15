// Package workspace contains immutable preparation and traceability models.
package workspace

import (
	"regexp"
	"sort"
	"strings"

	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/config"
)

var externalReferencePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]+-[0-9]+$`)

// FeatureContext is the non-graph context needed by later workspace reports.
type FeatureContext struct {
	ID                 string
	Title              string
	Description        string
	Design             string
	AcceptanceCriteria string
}

// TaskAssignment is one repository-owned work item and its accepted commit IDs.
type TaskAssignment struct {
	ID                 string
	ExternalRef        string
	Title              string
	Description        string
	Design             string
	AcceptanceCriteria string
	AllowedReferences  []string
}

// RepositoryAssignment groups work items by configured repository.
type RepositoryAssignment struct {
	Repository config.Repository
	Tasks      []TaskAssignment
}

// FeatureAssignments is the deterministic result of resolving one feature.
type FeatureAssignments struct {
	Feature      FeatureContext
	Repositories []RepositoryAssignment
}

// AssignmentError is a structured, secret-free resolver failure.
type AssignmentError struct {
	Code         string
	IssueID      string
	RepositoryID string
}

func (e AssignmentError) Error() string {
	switch e.Code {
	case "feature_inactive":
		return "feature " + e.IssueID + " is not active"
	case "feature_type":
		return "feature " + e.IssueID + " must be a feature"
	case "invalid_issue":
		return "task " + e.IssueID + " has an invalid ID"
	case "repository_label":
		return "task " + e.IssueID + " must have exactly one repository label"
	case "unknown_repository":
		return "task " + e.IssueID + " uses unknown repository " + e.RepositoryID
	case "blocked_dependency":
		return "task " + e.IssueID + " dependency " + e.RepositoryID + " is not closed"
	case "external_reference":
		return "task " + e.IssueID + " has invalid external reference"
	case "duplicate_issue":
		return "task " + e.IssueID + " appears more than once"
	default:
		return "task assignment resolution failed"
	}
}

// ResolveAssignments maps direct active feature children to configured
// repositories without calling Beads or mutating Git/filesystem state.
func ResolveAssignments(feature beads.Issue, cfg config.Config, children []beads.Issue) (FeatureAssignments, error) {
	if strings.TrimSpace(feature.ID) == "" {
		return FeatureAssignments{}, AssignmentError{Code: "feature_type", IssueID: "<missing>"}
	}
	if feature.Status == "closed" {
		return FeatureAssignments{}, AssignmentError{Code: "feature_inactive", IssueID: feature.ID}
	}
	if feature.Type != "feature" {
		return FeatureAssignments{}, AssignmentError{Code: "feature_type", IssueID: feature.ID}
	}

	repositories := make(map[string]config.Repository, len(cfg.Repositories))
	order := make([]string, 0, len(cfg.Repositories))
	for _, repository := range cfg.Repositories {
		repositories[repository.ID] = repository
		order = append(order, repository.ID)
	}
	grouped := make(map[string][]TaskAssignment)
	seen := make(map[string]struct{})
	for _, child := range children {
		if child.Parent != feature.ID || child.Status == "closed" || excludedRecord(child) {
			continue
		}
		if _, exists := seen[child.ID]; exists {
			return FeatureAssignments{}, AssignmentError{Code: "duplicate_issue", IssueID: child.ID}
		}
		seen[child.ID] = struct{}{}
		if strings.TrimSpace(child.ID) == "" {
			return FeatureAssignments{}, AssignmentError{Code: "invalid_issue", IssueID: "<missing>"}
		}
		repositoryID, ok := repositoryLabel(child.Labels)
		if !ok {
			return FeatureAssignments{}, AssignmentError{Code: "repository_label", IssueID: child.ID}
		}
		if _, ok := repositories[repositoryID]; !ok {
			return FeatureAssignments{}, AssignmentError{Code: "unknown_repository", IssueID: child.ID, RepositoryID: repositoryID}
		}
		for _, dependency := range child.Dependencies {
			if dependency.DependencyType == "parent-child" || dependency.Status == "closed" {
				continue
			}
			return FeatureAssignments{}, AssignmentError{Code: "blocked_dependency", IssueID: child.ID, RepositoryID: dependency.ID}
		}
		if child.ExternalRef != "" && !externalReferencePattern.MatchString(child.ExternalRef) {
			return FeatureAssignments{}, AssignmentError{Code: "external_reference", IssueID: child.ID}
		}
		allowed := []string{child.ID}
		if child.ExternalRef != "" {
			allowed = append(allowed, child.ExternalRef)
		}
		grouped[repositoryID] = append(grouped[repositoryID], TaskAssignment{
			ID:                 child.ID,
			ExternalRef:        child.ExternalRef,
			Title:              child.Title,
			Description:        child.Description,
			Design:             child.Design,
			AcceptanceCriteria: child.AcceptanceCriteria,
			AllowedReferences:  allowed,
		})
	}

	assignments := FeatureAssignments{
		Feature: FeatureContext{
			ID:                 feature.ID,
			Title:              feature.Title,
			Description:        feature.Description,
			Design:             feature.Design,
			AcceptanceCriteria: feature.AcceptanceCriteria,
		},
		Repositories: make([]RepositoryAssignment, 0, len(grouped)),
	}
	for _, repositoryID := range order {
		tasks := grouped[repositoryID]
		if len(tasks) == 0 {
			continue
		}
		sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
		assignments.Repositories = append(assignments.Repositories, RepositoryAssignment{Repository: repositories[repositoryID], Tasks: tasks})
	}
	return assignments, nil
}

func excludedRecord(issue beads.Issue) bool {
	if issue.Type == "decision" {
		return true
	}
	for _, label := range issue.Labels {
		switch label {
		case "human-review", "e2e", "human-decision", "decision":
			return true
		}
	}
	return false
}

func repositoryLabel(labels []string) (string, bool) {
	var matches []string
	for _, label := range labels {
		if strings.HasPrefix(label, "repo:") && strings.TrimPrefix(label, "repo:") != "" {
			matches = append(matches, strings.TrimPrefix(label, "repo:"))
		}
	}
	if len(matches) != 1 {
		return "", false
	}
	return matches[0], true
}
