// Package operations provides read-only workspace operations.
package operations

import (
	"context"

	"github.com/parmcoder/smt/internal/config"
	"github.com/parmcoder/smt/internal/git"
	"github.com/parmcoder/smt/internal/hooks"
)

// Entry is the JSON-marshallable state and diagnostic result for one repository.
type Entry struct {
	ID          string           `json:"id"`
	Path        string           `json:"path"`
	Initialized bool             `json:"initialized"`
	Dirty       bool             `json:"dirty"`
	Detached    bool             `json:"detached"`
	Branch      string           `json:"branch"`
	HeadSHA     string           `json:"head_sha"`
	HookStatus  hooks.HookStatus `json:"hook_status"`
	Error       string           `json:"error,omitempty"`
}

// HookInspector reports the local hook state for one repository path.
type HookInspector func(string) (hooks.HookStatus, error)

// Service reports configured repository state without changing the workspace.
type Service struct {
	config config.Config
	hooks  HookInspector
}

// New creates a read-only status service for cfg using runner for Git commands.
func New(cfg config.Config) *Service {
	return NewWithHookInspector(cfg, hooks.InspectCommitMsg)
}

// NewWithHookInspector creates a status service with an injected local hook inspector.
func NewWithHookInspector(cfg config.Config, inspector HookInspector) *Service {
	if inspector == nil {
		inspector = hooks.InspectCommitMsg
	}
	return &Service{config: cfg, hooks: inspector}
}

// Status inspects repositories in their configured order and records each
// repository's diagnostic error without stopping the remaining inspections.
func (s *Service) Status(ctx context.Context) ([]Entry, error) {
	entries := make([]Entry, 0, len(s.config.Repositories))
	for _, repository := range s.config.Repositories {
		entry := Entry{ID: repository.ID, Path: repository.Path}
		if s.hooks != nil {
			hookStatus, hookErr := s.hooks(repository.Path)
			entry.HookStatus = hookStatus
			if hookErr != nil {
				appendDiagnostic(&entry, hookErr)
			}
		}
		state, err := git.Inspect(ctx, git.Repository{
			ID:  repository.ID,
			Dir: repository.Path,
		})
		entry.Initialized = state.Initialized
		entry.Dirty = state.Dirty
		entry.Detached = state.Detached
		entry.Branch = state.Branch
		if err != nil {
			appendDiagnostic(&entry, err)
			entries = append(entries, entry)
			continue
		}

		if state.Initialized && !state.Detached {
			head, headErr := git.HeadSHA(ctx, git.Repository{ID: repository.ID, Dir: repository.Path})
			if headErr != nil {
				appendDiagnostic(&entry, headErr)
			} else {
				entry.HeadSHA = head
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func appendDiagnostic(entry *Entry, err error) {
	if err == nil {
		return
	}
	if entry.Error == "" {
		entry.Error = err.Error()
		return
	}
	entry.Error += "; " + err.Error()
}
