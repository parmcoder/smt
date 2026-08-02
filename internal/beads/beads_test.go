package beads

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"

	ggit "github.com/go-git/go-git/v5"
)

type commandCall struct {
	dir, name string
	args      []string
}

type fakeRunner struct {
	calls   []commandCall
	output  map[string]string
	failure map[string]error
}

func (f *fakeRunner) Run(_ context.Context, dir, name string, args ...string) (string, error) {
	f.calls = append(f.calls, commandCall{dir: dir, name: name, args: append([]string(nil), args...)})
	key := commandKey(name, args...)
	if err := f.failure[key]; err != nil {
		return "", err
	}
	return f.output[key], nil
}

func commandKey(name string, args ...string) string { return name + " " + strings.Join(args, " ") }

func reply(f *fakeRunner, output string, args ...string) {
	f.output[commandKey("bd", append(args, "--json")...)] = output
}

func fail(f *fakeRunner, err error, args ...string) {
	f.failure[commandKey("bd", append(args, "--json")...)] = err
}

func newFake() *fakeRunner {
	return &fakeRunner{output: map[string]string{}, failure: map[string]error{}}
}

func callsEqual(t *testing.T, f *fakeRunner, want ...string) {
	t.Helper()
	if len(f.calls) != len(want) {
		t.Fatalf("calls=%#v want=%v", f.calls, want)
	}
	for i, call := range f.calls {
		if call.dir != "/workspace" || commandKey(call.name, call.args...) != want[i] {
			t.Fatalf("call[%d]=%q dir=%q want=%q", i, commandKey(call.name, call.args...), call.dir, want[i])
		}
	}
}

const featureJSON = `[{"id":"feat-1","status":"open","issue_type":"feature"}]`
const queuedReviewJSON = `[{"id":"review-1","parent":"feat-1","status":"open","issue_type":"task","labels":["human-review","e2e","queued"]}]`
const failedReviewJSON = `[{"id":"review-1","parent":"feat-1","status":"open","issue_type":"task","labels":["human-review","e2e","failed","keep-me"]}]`

func TestQueueReviewCreatesAndWiresReview(t *testing.T) {
	fake := newFake()
	reply(fake, featureJSON, "show", "feat-1")
	reply(fake, `[]`, "list", "--parent", "feat-1", "--status", "open")
	reply(fake, `{"id":"review-1","status":"open","issue_type":"task","labels":["human-review","e2e","queued"]}`, "create", "Human E2E review for feat-1", "--type", "task", "--parent", "feat-1", "--labels", "human-review,e2e,queued", "--description", "handoff: docs/handoff.md\nevidence: docs/evidence.md")
	reply(fake, `{}`, "dep", "add", "feat-1", "review-1", "--type", "blocks")

	got, err := New("/workspace", fake).QueueReview(context.Background(), "feat-1", "docs/handoff.md", "docs/evidence.md")
	if err != nil || got.ReviewID != "review-1" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	callsEqual(t, fake,
		"bd show feat-1 --json",
		"bd list --parent feat-1 --status open --json",
		"bd create Human E2E review for feat-1 --type task --parent feat-1 --labels human-review,e2e,queued --description handoff: docs/handoff.md\nevidence: docs/evidence.md --json",
		"bd dep add feat-1 review-1 --type blocks --json",
	)
}

func TestQueueReviewRetryReconcilesExistingEdge(t *testing.T) {
	fake := newFake()
	reply(fake, featureJSON, "show", "feat-1")
	reply(fake, queuedReviewJSON, "list", "--parent", "feat-1", "--status", "open")
	reply(fake, `{}`, "dep", "add", "feat-1", "review-1", "--type", "blocks")
	got, err := New("/workspace", fake).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md")
	if err != nil || got.ReviewID != "review-1" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	callsEqual(t, fake, "bd show feat-1 --json", "bd list --parent feat-1 --status open --json", "bd dep add feat-1 review-1 --type blocks --json")
}

func TestQueueReviewPartialFailureReturnsRecovery(t *testing.T) {
	fake := newFake()
	reply(fake, featureJSON, "show", "feat-1")
	reply(fake, `[]`, "list", "--parent", "feat-1", "--status", "open")
	reply(fake, `{"id":"review-1"}`, "create", "Human E2E review for feat-1", "--type", "task", "--parent", "feat-1", "--labels", "human-review,e2e,queued", "--description", "handoff: docs/h.md\nevidence: docs/e.md")
	fail(fake, errors.New("secret command output"), "dep", "add", "feat-1", "review-1", "--type", "blocks")
	got, err := New("/workspace", fake).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md")
	if err == nil || got.ReviewID != "review-1" || !strings.Contains(got.Recovery, "bd dep add") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("result=%+v err=%v", got, err)
	}
}

func TestQueueReviewCreationAmbiguityReturnsFeatureRecovery(t *testing.T) {
	for _, test := range []struct {
		name   string
		output string
		fail   error
	}{
		{name: "command failure", fail: errors.New("private bd output")},
		{name: "malformed creation response", output: "[not JSON"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFake()
			reply(fake, featureJSON, "show", "feat-1")
			reply(fake, `[]`, "list", "--parent", "feat-1", "--status", "open")
			createArgs := []string{"create", "Human E2E review for feat-1", "--type", "task", "--parent", "feat-1", "--labels", "human-review,e2e,queued", "--description", "handoff: docs/h.md\nevidence: docs/e.md"}
			if test.fail != nil {
				fail(fake, test.fail, createArgs...)
			} else {
				reply(fake, test.output, createArgs...)
			}
			result, err := New("/workspace", fake).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md")
			if err == nil || result.FeatureID != "feat-1" || !strings.Contains(result.Recovery, "retry QueueReview") || strings.Contains(err.Error(), "private") {
				t.Fatalf("result=%+v err=%v", result, err)
			}
		})
	}
}

func TestQueueReviewRejectsClosedAndUnsafeInputs(t *testing.T) {
	service := New("/workspace", newFake())
	if _, err := service.QueueReview(context.Background(), "feat-1", "../handoff", "docs/e.md"); err == nil {
		t.Fatal("expected unsafe path error")
	}
	fake := newFake()
	reply(fake, `[{"id":"feat-1","status":"closed","issue_type":"feature"}]`, "show", "feat-1")
	if _, err := New("/workspace", fake).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md"); err == nil {
		t.Fatal("expected closed feature error")
	}
}

func TestQueueReviewRejectsDuplicateReviewsAndInvalidResponseIDs(t *testing.T) {
	fake := newFake()
	reply(fake, featureJSON, "show", "feat-1")
	reply(fake, `[{"id":"review-1","status":"open","labels":["human-review","e2e","queued"]},{"id":"review-2","status":"open","labels":["human-review","e2e","queued"]}]`, "list", "--parent", "feat-1", "--status", "open")
	if _, err := New("/workspace", fake).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md"); err == nil {
		t.Fatal("expected duplicate review error")
	}

	fake = newFake()
	reply(fake, featureJSON, "show", "feat-1")
	reply(fake, `[{"id":"","status":"open"}]`, "list", "--parent", "feat-1", "--status", "open")
	if _, err := New("/workspace", fake).QueueReview(context.Background(), "feat-1", "docs/h.md", "docs/e.md"); err == nil {
		t.Fatal("expected invalid issue response error")
	}
}

func TestHumanPassRejectsActiveBugsAndClosesOnlyAfterEvidence(t *testing.T) {
	fake := newFake()
	reply(fake, `[{"id":"review-1","status":"open","issue_type":"task","labels":["human-review","e2e","queued"],"dependencies":[{"id":"blocker-1","status":"open","dependency_type":"blocks"}]}]`, "show", "review-1")
	if _, err := New("/workspace", fake).HumanPass(context.Background(), "review-1", "Ada", "docs/evidence.md"); err == nil {
		t.Fatal("expected active bug blocker")
	}

	fake = newFake()
	reply(fake, queuedReviewJSON, "show", "review-1")
	reply(fake, `[{"id":"bug-1","status":"closed","labels":["review-bug"]}]`, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
	reply(fake, `[]`, "update", "review-1", "--append-notes", "reviewer: Ada\nevidence: docs/evidence.md")
	reply(fake, `[]`, "close", "review-1", "--reason", "human E2E passed")
	if _, err := New("/workspace", fake).HumanPass(context.Background(), "review-1", "Ada", "docs/evidence.md"); err != nil {
		t.Fatal(err)
	}
	callsEqual(t, fake,
		"bd show review-1 --json",
		"bd list --all --label review-bug --metadata-field review_id=review-1 --json",
		"bd update review-1 --append-notes reviewer: Ada\nevidence: docs/evidence.md --json",
		"bd close review-1 --reason human E2E passed --json",
	)
}

func TestHumanPassRequiresQueuedStateAndReturnsPartialRecovery(t *testing.T) {
	fake := newFake()
	reply(fake, failedReviewJSON, "show", "review-1")
	if _, err := New("/workspace", fake).HumanPass(context.Background(), "review-1", "Ada", "docs/e.md"); err == nil {
		t.Fatal("expected failed review rejection")
	}

	fake = newFake()
	reply(fake, queuedReviewJSON, "show", "review-1")
	reply(fake, `[]`, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
	fail(fake, errors.New("do not disclose"), "update", "review-1", "--append-notes", "reviewer: Ada\nevidence: docs/e.md")
	recovery, err := New("/workspace", fake).HumanPass(context.Background(), "review-1", "Ada", "docs/e.md")
	if err == nil || recovery.ReviewID != "review-1" || !strings.Contains(recovery.Recovery, "bd update") || strings.Contains(err.Error(), "disclose") {
		t.Fatalf("recovery=%+v err=%v", recovery, err)
	}

	fake = newFake()
	reply(fake, queuedReviewJSON, "show", "review-1")
	reply(fake, `[]`, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
	reply(fake, `[]`, "update", "review-1", "--append-notes", "reviewer: Ada\nevidence: docs/e.md")
	fail(fake, errors.New("do not disclose"), "close", "review-1", "--reason", "human E2E passed")
	recovery, err = New("/workspace", fake).HumanPass(context.Background(), "review-1", "Ada", "docs/e.md")
	if err == nil || recovery.ReviewID != "review-1" || !strings.Contains(recovery.Recovery, "bd close") {
		t.Fatalf("recovery=%+v err=%v", recovery, err)
	}
}

func TestHumanFailMarksFailedCreatesBugAndRetryWiresExistingBug(t *testing.T) {
	input := FailureInput{Title: "Cannot save", Reproduction: "open form", Expected: "saved", Actual: "error", Evidence: "docs/e.md"}
	fake := newFake()
	reply(fake, failedReviewJSON, "show", "review-1")
	reply(fake, featureJSON, "show", "feat-1")
	reply(fake, `[]`, "update", "review-1", "--set-labels", "human-review,e2e,keep-me,failed")
	reply(fake, `[]`, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
	reply(fake, `{"id":"bug-1","status":"open","labels":["review-bug"]}`, "create", "Cannot save", "--type", "bug", "--parent", "feat-1", "--no-inherit-labels", "--labels", "review-bug", "--metadata", `{"review_id":"review-1"}`, "--deps", "discovered-from:review-1", "--description", "reproduction: open form\nexpected: saved\nactual: error\nevidence: docs/e.md")
	reply(fake, `{}`, "dep", "add", "review-1", "bug-1", "--type", "blocks")
	got, err := New("/workspace", fake).HumanFail(context.Background(), "review-1", input)
	if err != nil || got.BugID != "bug-1" {
		t.Fatalf("result=%+v err=%v", got, err)
	}
	callsEqual(t, fake,
		"bd show review-1 --json",
		"bd show feat-1 --json",
		"bd update review-1 --set-labels human-review,e2e,keep-me,failed --json",
		"bd list --all --label review-bug --metadata-field review_id=review-1 --json",
		"bd create Cannot save --type bug --parent feat-1 --no-inherit-labels --labels review-bug --metadata {\"review_id\":\"review-1\"} --deps discovered-from:review-1 --description reproduction: open form\nexpected: saved\nactual: error\nevidence: docs/e.md --json",
		"bd dep add review-1 bug-1 --type blocks --json",
	)

	fake = newFake()
	reply(fake, failedReviewJSON, "show", "review-1")
	reply(fake, featureJSON, "show", "feat-1")
	reply(fake, `[]`, "update", "review-1", "--set-labels", "human-review,e2e,keep-me,failed")
	reply(fake, `[{"id":"bug-1","status":"open","labels":["review-bug"]}]`, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
	reply(fake, `{}`, "dep", "add", "review-1", "bug-1", "--type", "blocks")
	got, err = New("/workspace", fake).HumanFail(context.Background(), "review-1", input)
	if err != nil || got.BugID != "bug-1" {
		t.Fatalf("retry result=%+v err=%v", got, err)
	}
}

func TestHumanFailValidatesFeatureParentBeforeMarkingReviewFailed(t *testing.T) {
	fake := newFake()
	reply(fake, failedReviewJSON, "show", "review-1")
	reply(fake, `[{"id":"feat-1","status":"closed","issue_type":"feature"}]`, "show", "feat-1")
	_, err := New("/workspace", fake).HumanFail(context.Background(), "review-1", FailureInput{Title: "bug", Reproduction: "step", Expected: "ok", Actual: "no", Evidence: "docs/e.md"})
	if err == nil {
		t.Fatal("expected closed parent error")
	}
	callsEqual(t, fake, "bd show review-1 --json", "bd show feat-1 --json")
}

func TestHumanFailPostMutationFailuresReturnActionableRecovery(t *testing.T) {
	input := FailureInput{Title: "bug", Reproduction: "step", Expected: "ok", Actual: "no", Evidence: "docs/e.md"}
	for _, test := range []struct {
		name       string
		list       string
		create     string
		failList   error
		failCreate error
	}{
		{name: "linked bug list fails", failList: errors.New("private list output")},
		{name: "bug create fails", list: `[]`, failCreate: errors.New("private create output")},
		{name: "bug create response is malformed", list: `[]`, create: "[not JSON"},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFake()
			reply(fake, failedReviewJSON, "show", "review-1")
			reply(fake, featureJSON, "show", "feat-1")
			reply(fake, `[]`, "update", "review-1", "--set-labels", "human-review,e2e,keep-me,failed")
			listArgs := []string{"list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1"}
			if test.failList != nil {
				fail(fake, test.failList, listArgs...)
			} else {
				reply(fake, test.list, listArgs...)
				createArgs := []string{"create", "bug", "--type", "bug", "--parent", "feat-1", "--no-inherit-labels", "--labels", "review-bug", "--metadata", `{"review_id":"review-1"}`, "--deps", "discovered-from:review-1", "--description", "reproduction: step\nexpected: ok\nactual: no\nevidence: docs/e.md"}
				if test.failCreate != nil {
					fail(fake, test.failCreate, createArgs...)
				} else {
					reply(fake, test.create, createArgs...)
				}
			}
			recovery, err := New("/workspace", fake).HumanFail(context.Background(), "review-1", input)
			if err == nil || recovery.ReviewID != "review-1" || !strings.Contains(recovery.Recovery, "retry HumanFail") || strings.Contains(err.Error(), "private") {
				t.Fatalf("recovery=%+v err=%v", recovery, err)
			}
		})
	}
}

func TestHumanFailRejectsDuplicateActiveLinkedBugs(t *testing.T) {
	fake := newFake()
	reply(fake, failedReviewJSON, "show", "review-1")
	reply(fake, featureJSON, "show", "feat-1")
	reply(fake, `[]`, "update", "review-1", "--set-labels", "human-review,e2e,keep-me,failed")
	reply(fake, `[{"id":"bug-1","status":"open","labels":["review-bug"]},{"id":"bug-2","status":"in_progress","labels":["review-bug"]}]`, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
	recovery, err := New("/workspace", fake).HumanFail(context.Background(), "review-1", FailureInput{Title: "bug", Reproduction: "step", Expected: "ok", Actual: "no", Evidence: "docs/e.md"})
	if err == nil || recovery.ReviewID != "review-1" || !strings.Contains(recovery.Recovery, "inspect linked") {
		t.Fatalf("recovery=%+v err=%v", recovery, err)
	}
	callsEqual(t, fake,
		"bd show review-1 --json",
		"bd show feat-1 --json",
		"bd update review-1 --set-labels human-review,e2e,keep-me,failed --json",
		"bd list --all --label review-bug --metadata-field review_id=review-1 --json",
	)
}

func TestRequeueRequiresClosedBugAndPreservesLabels(t *testing.T) {
	fake := newFake()
	reply(fake, failedReviewJSON, "show", "review-1")
	reply(fake, `[{"id":"bug-1","status":"closed","labels":["review-bug"]}]`, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
	reply(fake, `[]`, "update", "review-1", "--status", "open", "--set-labels", "human-review,e2e,keep-me,retest-queued")
	if _, err := New("/workspace", fake).RequeueAfterFix(context.Background(), "review-1"); err != nil {
		t.Fatal(err)
	}

	fake = newFake()
	reply(fake, failedReviewJSON, "show", "review-1")
	reply(fake, `[{"id":"bug-1","status":"blocked","labels":["review-bug"]}]`, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
	if _, err := New("/workspace", fake).RequeueAfterFix(context.Background(), "review-1"); err == nil {
		t.Fatal("expected active bug error")
	}

	fake = newFake()
	reply(fake, failedReviewJSON, "show", "review-1")
	reply(fake, `[]`, "list", "--all", "--label", "review-bug", "--metadata-field", "review_id=review-1")
	if _, err := New("/workspace", fake).RequeueAfterFix(context.Background(), "review-1"); err == nil {
		t.Fatal("expected missing linked bug error")
	}
}

func TestReadyWorkAndReleaseReadinessUseCanonicalStates(t *testing.T) {
	fake := newFake()
	reply(fake, `[{"id":"feature-1","status":"open","issue_type":"feature"},{"id":"review-1","status":"open","labels":["human-review","e2e"]},{"id":"bug-1","status":"open","labels":["review-bug"]}]`, "list", "--ready")
	ready, err := New("/workspace", fake).ReadyWork(context.Background())
	if err != nil || len(ready) != 1 || ready[0].ID != "feature-1" {
		t.Fatalf("ready=%+v err=%v", ready, err)
	}

	fake = newFake()
	reply(fake, `[{"id":"review-1","status":"open","labels":["human-review","e2e"]},{"id":"bug-1","status":"in_progress","labels":["review-bug"]}]`, "list", "--status", "open,in_progress,blocked,deferred")
	readiness, err := New("/workspace", fake).ReleaseReadiness(context.Background())
	if err != nil || readiness.Ready || len(readiness.Blocking) != 2 {
		t.Fatalf("readiness=%+v err=%v", readiness, err)
	}
}

func TestListReviewsExposesDerivedReviewState(t *testing.T) {
	fake := newFake()
	reply(fake, `[{"id":"review-1","status":"open","labels":["human-review","e2e","retest-queued"]}]`, "list", "--label", "human-review", "--label", "e2e", "--status", "open")
	issues, err := New("/workspace", fake).ListReviews(context.Background())
	if err != nil || len(issues) != 1 || issues[0].ReviewState != "retest-queued" {
		t.Fatalf("issues=%+v err=%v", issues, err)
	}
}

func TestClientPreservesContextCancellationAndRejectsMalformedJSON(t *testing.T) {
	fake := newFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fail(fake, context.Canceled, "list", "--ready")
	if _, err := New("/workspace", fake).ReadyWork(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}

	fake = newFake()
	reply(fake, "[secret payload", "list", "--ready")
	if _, err := New("/workspace", fake).ReadyWork(context.Background()); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatalf("err=%v", err)
	}
}

func TestBeadsLifecycleIntegration(t *testing.T) {
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd is not installed")
	}
	dir := t.TempDir()
	if _, err := ggit.PlainInit(dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := runBD(t, dir, "init", "--non-interactive", "--init-if-missing", "--quiet", "--skip-agents", "--skip-hooks", "--prefix", "reviewtest"); err != nil {
		t.Fatal(err)
	}
	featureID := createFeature(t, dir, "First feature")
	service := New(dir, CommandRunner{})
	queued, err := service.QueueReview(context.Background(), featureID, "docs/handoff.md", "docs/evidence.md")
	if err != nil {
		t.Fatal(err)
	}
	assertBlockedRelease(t, service)
	failed, err := service.HumanFail(context.Background(), queued.ReviewID, FailureInput{
		Title: "The E2E flow fails", Reproduction: "run the E2E", Expected: "passes", Actual: "fails", Evidence: "docs/e2e.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	bug, err := service.show(context.Background(), failed.BugID)
	if err != nil {
		t.Fatal(err)
	}
	if bug.Parent != featureID || len(bug.Labels) != 1 || bug.Labels[0] != "review-bug" || metadataValue(bug, "review_id") != queued.ReviewID {
		t.Fatalf("bug parent=%q labels=%v metadata=%v", bug.Parent, bug.Labels, bug.Metadata)
	}
	if !hasDependency(bug, queued.ReviewID, "discovered-from") {
		t.Fatalf("bug dependencies=%+v", bug.Dependencies)
	}
	reviewIssue, err := service.show(context.Background(), queued.ReviewID)
	if err != nil || stateOf(reviewIssue) != "failed" || !hasDependency(reviewIssue, failed.BugID, "blocks") {
		t.Fatalf("failed review=%+v err=%v", reviewIssue, err)
	}
	assertBlockedRelease(t, service)
	if _, err := runBD(t, dir, "close", failed.BugID, "--reason", "fixed"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RequeueAfterFix(context.Background(), queued.ReviewID); err != nil {
		t.Fatal(err)
	}
	reviewIssue, err = service.show(context.Background(), queued.ReviewID)
	if err != nil || stateOf(reviewIssue) != "retest-queued" {
		t.Fatalf("requeued review=%+v err=%v", reviewIssue, err)
	}
	if _, err := service.HumanPass(context.Background(), queued.ReviewID, "Human reviewer", "docs/retest.md"); err != nil {
		t.Fatal(err)
	}
	readiness, err := service.ReleaseReadiness(context.Background())
	if err != nil || !readiness.Ready {
		t.Fatalf("post-pass readiness=%+v err=%v", readiness, err)
	}

	directFeatureID := createFeature(t, dir, "Direct pass feature")
	direct, err := service.QueueReview(context.Background(), directFeatureID, "docs/direct.md", "docs/direct-evidence.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.HumanPass(context.Background(), direct.ReviewID, "Human reviewer", "docs/direct-evidence.md"); err != nil {
		t.Fatal(err)
	}
	cycles, err := runBD(t, dir, "dep", "cycles")
	if err != nil || strings.TrimSpace(cycles) != "[]" {
		t.Fatalf("cycles=%q err=%v", cycles, err)
	}
}

func assertBlockedRelease(t *testing.T, service *Service) {
	t.Helper()
	readiness, err := service.ReleaseReadiness(context.Background())
	if err != nil || readiness.Ready || len(readiness.Blocking) == 0 {
		t.Fatalf("release readiness=%+v err=%v", readiness, err)
	}
}

func hasDependency(issue Issue, id, kind string) bool {
	for _, dependency := range issue.Dependencies {
		if dependency.ID == id && dependency.DependencyType == kind {
			return true
		}
	}
	return false
}

func createFeature(t *testing.T, dir, title string) string {
	t.Helper()
	output, err := runBD(t, dir, "create", title, "--type", "feature")
	if err != nil {
		t.Fatal(err)
	}
	var issue Issue
	if err := json.Unmarshal([]byte(output), &issue); err != nil || issue.ID == "" {
		t.Fatalf("create feature output=%q err=%v", output, err)
	}
	return issue.ID
}

func runBD(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	command := exec.Command("bd", append(args, "--json")...)
	command.Dir = dir
	output, err := command.Output()
	return string(output), err
}
