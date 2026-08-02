// Package git provides SMT's pure-Go Git operations.
package git

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	ggit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

type Repository struct {
	ID, Dir string
	IsRoot  bool
}

type State struct {
	Branch                       string
	Detached, Dirty, Initialized bool
	ChangedFiles                 []string
}

type CommitMessage struct{ SHA, Message string }

func Open(dir string) (*ggit.Repository, error) { return ggit.PlainOpen(dir) }

func Inspect(ctx context.Context, repository Repository) (State, error) {
	if err := ctx.Err(); err != nil {
		return State{}, err
	}
	r, err := Open(repository.Dir)
	if errors.Is(err, ggit.ErrRepositoryNotExists) {
		return State{}, nil
	}
	if err != nil {
		return State{}, repositoryError("open repository", repository, err)
	}
	state := State{Initialized: true}
	wt, err := r.Worktree()
	if err != nil {
		return State{}, repositoryError("open worktree", repository, err)
	}
	status, err := wt.Status()
	if err != nil {
		return State{}, repositoryError("inspect status", repository, err)
	}
	for path, file := range status {
		if file.Staging == ggit.Untracked && harmlessUntrackedPath(path) {
			continue
		}
		if file.Staging != ggit.Unmodified || file.Worktree != ggit.Unmodified {
			state.ChangedFiles = append(state.ChangedFiles, path)
		}
	}
	sort.Strings(state.ChangedFiles)
	state.Dirty = len(state.ChangedFiles) != 0
	head, err := r.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		state.Detached = true
		return state, nil
	}
	if err != nil {
		return State{}, repositoryError("read HEAD", repository, err)
	}
	if !head.Name().IsBranch() {
		state.Detached = true
		return state, nil
	}
	state.Branch = head.Name().Short()
	return state, nil
}

func ChangedFiles(ctx context.Context, repository Repository) ([]string, error) {
	state, err := Inspect(ctx, repository)
	if err != nil {
		return nil, err
	}
	return state.ChangedFiles, nil
}

func HeadSHA(ctx context.Context, repository Repository) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	r, err := Open(repository.Dir)
	if err != nil {
		return "", repositoryError("open repository", repository, err)
	}
	head, err := r.Head()
	if err != nil {
		return "", repositoryError("read HEAD", repository, err)
	}
	return head.Hash().String(), nil
}

func CommitMessages(ctx context.Context, repository Repository, from, to string) ([]CommitMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r, err := Open(repository.Dir)
	if err != nil {
		return nil, repositoryError("open repository", repository, err)
	}
	toHash, err := r.ResolveRevision(plumbing.Revision(to))
	if err != nil {
		return nil, repositoryError("resolve commit range", repository, err)
	}
	fromHash, err := r.ResolveRevision(plumbing.Revision(from))
	if err != nil {
		return nil, repositoryError("resolve commit range", repository, err)
	}
	excluded, err := reachableCommits(r, *fromHash)
	if err != nil {
		return nil, repositoryError("read commit messages", repository, err)
	}
	it, err := r.Log(&ggit.LogOptions{From: *toHash})
	if err != nil {
		return nil, repositoryError("read commit messages", repository, err)
	}
	defer it.Close()
	result := []CommitMessage{}
	err = it.ForEach(func(c *object.Commit) error {
		if _, found := excluded[c.Hash]; found {
			return nil
		}
		result = append(result, CommitMessage{SHA: c.Hash.String(), Message: strings.TrimSuffix(c.Message, "\n")})
		return nil
	})
	if err != nil {
		return nil, repositoryError("read commit messages", repository, err)
	}
	return result, nil
}

func reachableCommits(repository *ggit.Repository, from plumbing.Hash) (map[plumbing.Hash]struct{}, error) {
	it, err := repository.Log(&ggit.LogOptions{From: from})
	if err != nil {
		return nil, err
	}
	defer it.Close()
	commits := map[plumbing.Hash]struct{}{}
	err = it.ForEach(func(commit *object.Commit) error { commits[commit.Hash] = struct{}{}; return nil })
	return commits, err
}
func harmlessUntrackedPath(path string) bool {
	switch filepath.Base(path) {
	case ".DS_Store", "Thumbs.db", "desktop.ini":
		return true
	}
	return false
}
func repositoryError(operation string, repository Repository, err error) error {
	return fmt.Errorf("%s for repository %s at %s: %w", operation, repository.ID, repository.Dir, err)
}
