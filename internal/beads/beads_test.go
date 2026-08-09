package beads

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type call struct {
	name string
	args []string
}
type fakeRunner struct {
	calls    []call
	replies  map[string]string
	failures map[string]error
}

func newFake() *fakeRunner {
	return &fakeRunner{replies: map[string]string{}, failures: map[string]error{}}
}
func key(args ...string) string { return strings.Join(append(args, "--json"), " ") }
func (f *fakeRunner) Run(_ context.Context, _ string, _ string, args ...string) (string, error) {
	f.calls = append(f.calls, call{"bd", append([]string(nil), args...)})
	if err := f.failures[strings.Join(args, " ")]; err != nil {
		return "", err
	}
	return f.replies[strings.Join(args, " ")], nil
}
func reply(f *fakeRunner, value string, args ...string) { f.replies[key(args...)] = value }
func fail(f *fakeRunner, err error, args ...string)     { f.failures[key(args...)] = err }

const feature = `{"id":"feat-1","status":"open","issue_type":"feature"}`
const failedReview = `{"id":"review-1","status":"open","issue_type":"task","labels":["human-review","e2e","failed","keep-me"]}`

func TestClientRedactsRunnerAndMalformedJSON(t *testing.T) {
	f := newFake()
	fail(f, errors.New("token=secret"), "list", "--ready")
	if _, err := New("/workspace", f).ReadyWork(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("error=%v", err)
	}
	f = newFake()
	reply(f, "[private payload", "list", "--ready")
	if _, err := New("/workspace", f).ReadyWork(context.Background()); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("error=%v", err)
	}
}

func TestReadyWorkFiltersReviewRecords(t *testing.T) {
	f := newFake()
	reply(f, `[{"id":"feature","status":"open"},{"id":"review","labels":["human-review","e2e"]},{"id":"bug","labels":["review-bug"]}]`, "list", "--ready")
	got, err := New("/workspace", f).ReadyWork(context.Background())
	if err != nil || len(got) != 1 || got[0].ID != "feature" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestQueueReviewCreateDuplicateAndRecovery(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		f := newFake()
		reply(f, feature, "show", "feat-1")
		reply(f, `[]`, "list", "--parent", "feat-1", "--status", "open")
		reply(f, `{"id":"review-1"}`, "create", "Human E2E review for feat-1", "--type", "task", "--parent", "feat-1", "--labels", "human-review,e2e,queued", "--description", "handoff: docs/h.md\nevidence: docs/e.md")
		reply(f, `{}`, "dep", "add", "feat-1", "review-1", "--type", "blocks")
		got, err := New("/workspace", f).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md")
		if err != nil || got.ReviewID != "review-1" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})
	t.Run("duplicate", func(t *testing.T) {
		f := newFake()
		reply(f, feature, "show", "feat-1")
		reply(f, `[{"id":"review-1","labels":["human-review","e2e"]},{"id":"review-2","labels":["human-review","e2e"]}]`, "list", "--parent", "feat-1", "--status", "open")
		if _, err := New("/workspace", f).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md"); err == nil {
			t.Fatal("duplicate review accepted")
		}
	})
	t.Run("existing edge recovery", func(t *testing.T) {
		f := newFake()
		reply(f, feature, "show", "feat-1")
		reply(f, `[{"id":"review-1","labels":["human-review","e2e"]}]`, "list", "--parent", "feat-1", "--status", "open")
		fail(f, errors.New("private edge"), "dep", "add", "feat-1", "review-1", "--type", "blocks")
		got, err := New("/workspace", f).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md")
		if err == nil || got.Recovery != "run: bd dep add feat-1 review-1 --type blocks" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})
	t.Run("created edge recovery", func(t *testing.T) {
		f := newFake()
		reply(f, feature, "show", "feat-1")
		reply(f, `[]`, "list", "--parent", "feat-1", "--status", "open")
		reply(f, `{"id":"review-1"}`, "create", "Human E2E review for feat-1", "--type", "task", "--parent", "feat-1", "--labels", "human-review,e2e,queued", "--description", "handoff: docs/h.md\nevidence: docs/e.md")
		fail(f, errors.New("private edge"), "dep", "add", "feat-1", "review-1", "--type", "blocks")
		got, err := New("/workspace", f).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md")
		if err == nil || got.ReviewID != "review-1" || got.Recovery != "run: bd dep add feat-1 review-1 --type blocks" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	})
}

func TestListReleaseAndRequeueContracts(t *testing.T) {
	f := newFake()
	reply(f, `[{"id":"review-1","status":"open","issue_type":"task","labels":["human-review","e2e","retest-queued"]}]`, "list", "--label", "human-review", "--label", "e2e", "--status", "open")
	got, err := New("/workspace", f).ListReviews(context.Background())
	if err != nil || got[0].ReviewState != "retest-queued" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	f = newFake()
	reply(f, `[{"id":"review-1","status":"open","labels":["human-review","e2e"]},{"id":"bug-1","status":"blocked","labels":["review-bug"]}]`, "list", "--status", "open,in_progress,blocked,deferred")
	ready, err := New("/workspace", f).ReleaseReadiness(context.Background())
	if err != nil || ready.Ready || len(ready.Blocking) != 2 {
		t.Fatalf("got=%+v err=%v", ready, err)
	}
	for _, tc := range []struct {
		name, bugs string
		wantErr    bool
	}{{"no bug", `[]`, true}, {"active bug", `[{"id":"bug-1","status":"open","labels":["review-bug"]}]`, true}, {"closed bug", `[{"id":"bug-1","status":"closed","labels":["review-bug"]}]`, false}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFake()
			reply(f, failedReview, "show", "review-1")
			reply(f, tc.bugs, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
			if !tc.wantErr {
				reply(f, `{}`, "update", "review-1", "--status", "open", "--set-labels", "human-review,e2e,keep-me,retest-queued")
			}
			got, err := New("/workspace", f).RequeueAfterFix(context.Background(), "review-1")
			if (err != nil) != tc.wantErr || (!tc.wantErr && got.ReviewID != "review-1") {
				t.Fatalf("got=%+v err=%v", got, err)
			}
			if !tc.wantErr {
				last := f.calls[len(f.calls)-1]
				if strings.Join(last.args, " ") != "update review-1 --status open --set-labels human-review,e2e,keep-me,retest-queued --json" {
					t.Fatalf("labels not preserved: %#v", last.args)
				}
			}
		})
	}
	if _, err := New("/workspace", newFake()).RequeueAfterFix(context.Background(), "bad id!"); err == nil {
		t.Fatal("unsafe review ID accepted")
	}
}
