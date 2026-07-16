// Package commit validates SMT Conventional Commit messages.
package commit

import (
	"fmt"
	"strings"

	conventionalcommits "github.com/leodido/go-conventionalcommits"
	"github.com/leodido/go-conventionalcommits/parser"
)

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

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
