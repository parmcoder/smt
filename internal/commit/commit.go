// Package commit validates SMT Conventional Commit messages.
package commit

import (
	"fmt"
	"regexp"
	"strings"

	conventionalcommits "github.com/leodido/go-conventionalcommits"
	"github.com/leodido/go-conventionalcommits/parser"
)

var preparedHeaderPattern = regexp.MustCompile(`^[^:\s(]+\([^)]*\)!?:\s+\[([^\]\s]+)\]\s+\S`)
var preparedReferencePattern = regexp.MustCompile(`^(?:[A-Za-z0-9][A-Za-z0-9._-]*|[A-Z][A-Z0-9]+-[0-9]+)$`)

// Policy contains the configured commit types and scopes.
type Policy struct {
	Types  []string
	Scopes []string
}

// ValidateMessage strictly parses a complete message and applies SMT policy.
func ValidateMessage(message string, policy Policy) error {
	message = strings.TrimSuffix(message, "\n")
	message = strings.TrimSuffix(message, "\r")
	parsed, err := parser.NewMachine(parser.WithTypes(conventionalcommits.TypesConventional)).Parse([]byte(message))
	if err != nil || parsed == nil || !parsed.Ok() {
		if err != nil {
			return fmt.Errorf("invalid conventional commit: %w", err)
		}
		return fmt.Errorf("invalid conventional commit")
	}
	cc, ok := parsed.(*conventionalcommits.ConventionalCommit)
	if !ok {
		return fmt.Errorf("invalid conventional commit")
	}
	if !contains(policy.Types, cc.Type) {
		return fmt.Errorf("type %q is not allowed", cc.Type)
	}
	if cc.Scope == nil || strings.TrimSpace(*cc.Scope) == "" {
		return fmt.Errorf("scope is required")
	}
	for _, scope := range strings.Split(*cc.Scope, ",") {
		scope = strings.TrimSpace(scope)
		if scope == "" {
			return fmt.Errorf("scope contains an empty value")
		}
		if !contains(policy.Scopes, scope) {
			return fmt.Errorf("scope %q is not allowed", scope)
		}
	}
	return nil
}

// ValidateBranchMessage applies ordinary conventional syntax on the effective
// default branch and requires an exact Beads ID reference on other branches.
func ValidateBranchMessage(message string, policy Policy, branch, defaultBranch string) error {
	if strings.TrimSpace(branch) == "" {
		return fmt.Errorf("branch is required")
	}
	if strings.TrimSpace(defaultBranch) == "" {
		defaultBranch = "main"
	}
	if branch == defaultBranch {
		return ValidateMessage(message, policy)
	}
	reference, err := WorkItemReference(message)
	if err != nil {
		return fmt.Errorf("branch %q requires exact Beads ID reference", branch)
	}
	if reference != branch {
		return fmt.Errorf("commit reference must match branch")
	}
	return ValidateMessage(message, policy)
}

// WorkItemReference extracts and validates the bracketed Beads ID.
func WorkItemReference(message string) (string, error) {
	firstLine := strings.SplitN(strings.TrimSuffix(strings.TrimSuffix(message, "\n"), "\r"), "\n", 2)[0]
	match := preparedHeaderPattern.FindStringSubmatch(firstLine)
	if len(match) != 2 {
		return "", fmt.Errorf("Beads branch commit requires its task ID immediately after the conventional prefix")
	}
	if !preparedReferencePattern.MatchString(match[1]) {
		return "", fmt.Errorf("invalid work-item reference")
	}
	return match[1], nil
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
