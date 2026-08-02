package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	ggit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestInspectReportsAttachedCleanChangedFilesMetadataAndDetached(t *testing.T) {
	dir, repository := newRepository(t)
	state, err := Inspect(context.Background(), Repository{ID: "repo", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Initialized || state.Dirty || state.Detached || state.Branch == "" {
		t.Fatalf("clean state=%#v", state)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("unstaged"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("staged"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("staged.txt"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("untracked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".DS_Store"), []byte("metadata"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err = Inspect(context.Background(), Repository{ID: "repo", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Dirty || !reflect.DeepEqual(state.ChangedFiles, []string{"staged.txt", "tracked.txt", "untracked.txt"}) {
		t.Fatalf("changed state=%#v", state)
	}
	head, err := repository.Head()
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Storer.SetReference(plumbing.NewHashReference(plumbing.HEAD, head.Hash())); err != nil {
		t.Fatal(err)
	}
	state, err = Inspect(context.Background(), Repository{ID: "repo", Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !state.Detached || state.Branch != "" {
		t.Fatalf("detached state=%#v", state)
	}
}

func TestCommitMessagesUsesReachabilityForMergeRange(t *testing.T) {
	dir, repository := newRepository(t)
	base, err := repository.Head()
	if err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	from := commitFile(t, repository, worktree, dir, "from.txt", "from", "feat: from\n\nfull body")
	if err := repository.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("feature"), base.Hash())); err != nil {
		t.Fatal(err)
	}
	if err := worktree.Checkout(&ggit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("feature")}); err != nil {
		t.Fatal(err)
	}
	feature := commitFile(t, repository, worktree, dir, "feature.txt", "feature", "feat: feature")
	if err := worktree.Checkout(&ggit.CheckoutOptions{Branch: plumbing.NewBranchReferenceName("master")}); err != nil {
		t.Fatal(err)
	}
	main := commitFile(t, repository, worktree, dir, "main.txt", "main", "feat: main")
	merge := writeMergeCommit(t, repository, main, feature, "merge feature")
	messages, err := CommitMessages(context.Background(), Repository{ID: "repo", Dir: dir}, from.String(), merge.String())
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 {
		t.Fatalf("message count=%d messages=%#v", len(messages), messages)
	}
	if got := []string{messages[0].SHA, messages[1].SHA, messages[2].SHA}; !reflect.DeepEqual(got, []string{merge.String(), main.String(), feature.String()}) {
		t.Fatalf("messages=%#v", messages)
	}
	if messages[2].Message != "feat: feature" {
		t.Fatalf("message=%q", messages[2].Message)
	}
}

func newRepository(t *testing.T) (string, *ggit.Repository) {
	t.Helper()
	dir := t.TempDir()
	repository, err := ggit.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tracked.txt"), []byte("initial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("ignored.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktree, err := repository.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add("."); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Commit("initial", signature()); err != nil {
		t.Fatal(err)
	}
	return dir, repository
}
func signature() *ggit.CommitOptions {
	return &ggit.CommitOptions{Author: &object.Signature{Name: "test", Email: "test@example.invalid", When: time.Unix(1, 0)}}
}
func commitFile(t *testing.T, repository *ggit.Repository, worktree *ggit.Worktree, dir, path, contents, message string) plumbing.Hash {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := worktree.Add(path); err != nil {
		t.Fatal(err)
	}
	hash, err := worktree.Commit(message, signature())
	if err != nil {
		t.Fatal(err)
	}
	return hash
}
func writeMergeCommit(t *testing.T, repository *ggit.Repository, main, feature plumbing.Hash, message string) plumbing.Hash {
	t.Helper()
	mainCommit, err := repository.CommitObject(main)
	if err != nil {
		t.Fatal(err)
	}
	commit := &object.Commit{Author: object.Signature{Name: "test", Email: "test@example.invalid", When: time.Unix(2, 0)}, Committer: object.Signature{Name: "test", Email: "test@example.invalid", When: time.Unix(2, 0)}, Message: message, TreeHash: mainCommit.TreeHash, ParentHashes: []plumbing.Hash{main, feature}}
	encoded := repository.Storer.NewEncodedObject()
	if err := commit.Encode(encoded); err != nil {
		t.Fatal(err)
	}
	hash, err := repository.Storer.SetEncodedObject(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("master"), hash)); err != nil {
		t.Fatal(err)
	}
	return hash
}
