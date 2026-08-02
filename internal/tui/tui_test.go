package tui

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/prereq"
	"github.com/parmcoder/smt/internal/scaffold"
)

func TestModelNavigationResizeAndCancellation(t *testing.T) {
	m := NewModel(true)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 20, Height: 8})
	if !strings.Contains(next.(Model).View().Content, "too small") {
		t.Fatal("small terminal view")
	}
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if next.(Model).tab != 1 {
		t.Fatal("tab navigation")
	}
	cmd := m.Command(func(ctx context.Context) (any, error) { <-ctx.Done(); return nil, ctx.Err() })
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	if next.(Model).busy {
		t.Fatal("cancel did not clear busy")
	}
	_ = cmd
}

func TestNoColorViewHasNoANSI(t *testing.T) {
	if strings.Contains(NewModel(true).View().Content, "\x1b[") {
		t.Fatal("ANSI in no-color view")
	}
}

func TestSetupFormFocusEditToggleAndInvalidation(t *testing.T) {
	m := NewModel(true)
	if !m.destination.Focused() || !m.selection.Web || !m.selection.API || !m.selection.Database || !m.selection.DevOps || !m.selection.Codex {
		t.Fatal("setup defaults")
	}
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "x"}))
	m = next.(Model)
	if m.destination.Value() != ".x" {
		t.Fatalf("destination=%q", m.destination.Value())
	}
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = next.(Model)
	if m.destination.Focused() {
		t.Fatal("blur")
	}
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if m.selection.Web {
		t.Fatal("toggle")
	}
	if m.setupReady || m.setupResult != "" {
		t.Fatal("invalidation")
	}
}

func TestSetupRendererPlainStyledAndFallbacks(t *testing.T) {
	plain := NewModel(true)
	plain.width = 80
	plain.notice = "NOTICE-ONCE"
	content := plain.View().Content
	if strings.Contains(content, "\x1b") || strings.Count(content, "NOTICE-ONCE") != 1 || !strings.Contains(content, "[x] Web") || !strings.Contains(content, "q quit") {
		t.Fatalf("plain=%q", content)
	}
	styled := NewModel(false)
	styled.notice = "NOTICE-ONCE"
	styled.width = 100
	if styled.View().Content == "" || strings.Count(styled.View().Content, "NOTICE-ONCE") != 1 {
		t.Fatal("styled semantics")
	}
	narrow := NewModel(true)
	narrow.width = 39
	if !strings.Contains(narrow.View().Content, "too narrow") {
		t.Fatal("narrow fallback")
	}
	short := NewModel(true)
	short.width = 80
	short.height = 9
	if !strings.Contains(short.View().Content, "too short") {
		t.Fatal("height fallback")
	}
}

func TestSetupFocusCyclesAndTopNavigation(t *testing.T) {
	m := NewModel(true)
	for index, key := range []tea.Key{{Code: tea.KeyTab}, {Code: tea.KeyDown}, {Code: tea.KeyUp}, {Code: tea.KeyTab, Mod: tea.ModShift}} {
		next, _ := m.Update(tea.KeyPressMsg(key))
		m = next.(Model)
		want := []int{1, 2, 1, 0}[index]
		if m.setupFocus != want || m.destination.Focused() != (want == 0) {
			t.Fatalf("focus=%d focused=%t", m.setupFocus, m.destination.Focused())
		}
	}
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	if next.(Model).tab != 1 {
		t.Fatal("right nav")
	}
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	if next.(Model).tab != 0 {
		t.Fatal("left nav")
	}
}

func TestStyledRendererSemantics(t *testing.T) {
	m := NewModel(false)
	m.width = 100
	m.notice = "NOTICE-ONCE"
	raw := m.View().Content
	if !strings.Contains(raw, "\x1b") {
		t.Fatal("missing ANSI")
	}
	text := ansi.Strip(raw)
	for _, want := range []string{"SMT — Sanovy Mono Tool", "> Setup", "╭", "╯", "NOTICE-ONCE", "q quit"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestSetupAllToggles(t *testing.T) {
	m := NewModel(true)
	for focus := 1; focus <= 4; focus++ {
		m.setSetupFocus(focus)
		next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
		m = next.(Model)
	}
	if m.selection.Web || m.selection.API || m.selection.Database || m.selection.DevOps || !m.selection.Codex {
		t.Fatal("toggle values")
	}
}

type fakeOps struct {
	checks            []prereq.Requirements
	contexts          []context.Context
	result            prereq.Result
	err               error
	readyWork         []beads.Issue
	readyWorkErr      error
	readyWorkContexts []context.Context
	queueReviews      []struct{ featureID, handoff, evidence string }
	queueContexts     []context.Context
	queueResult       beads.QueueResult
	queueErr          error
	reviews           []beads.Issue
	reviewsErr        error
	reviewContexts    []context.Context
	inits             []struct {
		destination string
		selection   scaffold.Selection
	}
	initContexts []context.Context
}

func (f *fakeOps) Check(ctx context.Context, r prereq.Requirements) (prereq.Result, error) {
	f.checks = append(f.checks, r)
	f.contexts = append(f.contexts, ctx)
	return f.result, f.err
}

func (f *fakeOps) ReadyWork(ctx context.Context) ([]beads.Issue, error) {
	f.readyWorkContexts = append(f.readyWorkContexts, ctx)
	return f.readyWork, f.readyWorkErr
}

func (f *fakeOps) ListReviews(ctx context.Context) ([]beads.Issue, error) {
	f.reviewContexts = append(f.reviewContexts, ctx)
	return f.reviews, f.reviewsErr
}

func TestCheckEscapeCancelsCapturedContext(t *testing.T) {
	f := &fakeOps{}
	m := NewModelWithOperations(true, f)
	m.setupFocus = 5
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	old, version := m.opID, m.setupVersion
	_ = cmd()
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = next.(Model)
	if len(f.contexts) != 1 || f.contexts[0].Err() != context.Canceled || m.busy || m.cancel != nil || m.opID == old || m.notice != "Operation cancelled" {
		t.Fatalf("contexts=%d err=%v busy=%t cancel=%v old=%d now=%d notice=%q", len(f.contexts), f.contexts[0].Err(), m.busy, m.cancel, old, m.opID, m.notice)
	}
	next, _ = m.Update(resultMsg{value: prereq.Result{Findings: []prereq.Finding{{ID: "ready", Status: prereq.StatusReady}}}, id: old, setupVersion: version, kind: opCheck})
	m = next.(Model)
	if m.notice != "Operation cancelled" || m.setupReady || len(m.findings) != 0 || m.busy || m.cancel != nil {
		t.Fatal("late result changed cancellation")
	}
}

func TestSetupReadyFindingsRenderPlainAndInert(t *testing.T) {
	f := &fakeOps{}
	m := NewModelWithOperations(true, f)
	m.setupReady = true
	m.findings = []prereq.Finding{{ID: "codex", Status: prereq.StatusReady, Message: "Codex is available", Guidance: "human only"}, {ID: "asdf", Status: prereq.StatusReady, Message: "asdf is available"}}
	view := m.View().Content
	for _, want := range []string{"Re-check prerequisites", "Prerequisites are ready", "codex ready: Codex is available", "Guidance: human only", "asdf ready: asdf is available"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(view, "\x1b") || len(f.checks) != 0 {
		t.Fatal("view invoked adapter or ANSI")
	}
}

func TestSetupNotReadyFindingsAndErrorRenderSafely(t *testing.T) {
	for _, status := range []prereq.Status{prereq.StatusMissing, prereq.StatusMalformed} {
		t.Run(string(status), func(t *testing.T) {
			f := &fakeOps{}
			m := NewModelWithOperations(true, f)
			m.findings = []prereq.Finding{{ID: "tool", Status: status, Message: "not present", Guidance: "human guidance"}}
			view := m.View().Content
			for _, want := range []string{"Re-check prerequisites", "Prerequisites are not ready", "tool " + string(status) + ": not present", "Guidance: human guidance"} {
				if !strings.Contains(view, want) {
					t.Fatal(want)
				}
			}
			if strings.Contains(view, "\x1b") || len(f.checks) != 0 {
				t.Fatal("unsafe view")
			}
		})
	}
	m := NewModel(true)
	m.notice = "Operation failed safely"
	view := m.View().Content
	if m.setupReady || strings.Contains(view, "credential-secret") {
		t.Fatal("error leaked")
	}
}

func TestFindingsStyledAndPlainWidthSemantics(t *testing.T) {
	longID := "finding-" + strings.Repeat("X", 81)
	longMessage := "message-" + strings.Repeat("Y", 81)
	longGuidance := "guidance-" + strings.Repeat("Z", 81)
	findings := []prereq.Finding{{ID: longID, Status: prereq.StatusMissing, Message: longMessage, Guidance: longGuidance}, {ID: "empty", Status: prereq.StatusMalformed, Message: "empty guidance message"}}
	for _, width := range []int{60, 100} {
		for _, noColor := range []bool{true, false} {
			m := NewModel(noColor)
			m.width = width
			m.findings = findings
			raw := m.View().Content
			if noColor && strings.Contains(raw, "\x1b") {
				t.Fatal("plain ANSI")
			}
			if !noColor && !strings.Contains(raw, "\x1b") {
				t.Fatal("styled ANSI")
			}
			for _, line := range strings.Split(raw, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("width=%d line=%q", width, line)
				}
			}
			text := ansi.Strip(raw)
			normalized := strings.Join(strings.Fields(text), "")
			for _, want := range []string{"Re-check prerequisites", "Prerequisites are not ready", "missing", "empty malformed: empty guidance message"} {
				if !strings.Contains(text, want) {
					t.Fatal(want)
				}
			}
			if noColor {
				for _, want := range []string{longID, longMessage, "Guidance:" + longGuidance} {
					if !strings.Contains(normalized, want) {
						t.Fatal(want)
					}
				}
			} else {
				for _, prefix := range []string{"finding-", "message-", "Guidance:"} {
					if !strings.Contains(normalized, prefix) {
						t.Fatal(prefix)
					}
				}
				for _, char := range []string{"X", "Y", "Z"} {
					if strings.Count(text, char) != 81 {
						t.Fatalf("missing wrapped %s", char)
					}
				}
				if strings.Contains(text, "...") {
					t.Fatal("ellipsis")
				}
			}
			if strings.Count(text, "Guidance:") != 1 {
				t.Fatal("empty guidance rendered")
			}
			if strings.Contains(text, "Guidance: \nempty") {
				t.Fatal("empty guidance rendered")
			}
		}
	}
}
func (f *fakeOps) Init(ctx context.Context, destination string, selection scaffold.Selection) (scaffold.Result, error) {
	f.initContexts = append(f.initContexts, ctx)
	f.inits = append(f.inits, struct {
		destination string
		selection   scaffold.Selection
	}{destination, selection})
	return scaffold.Result{}, nil
}

func TestSetupInitEscapeCancelsAndIgnoresLateSuccess(t *testing.T) {
	f := &fakeOps{}
	m := NewModelWithOperations(true, f)
	m.setupReady = true
	m.setupFocus = 6
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	old, version := m.opID, m.setupVersion
	_ = cmd()
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = next.(Model)
	if len(f.initContexts) != 1 || f.initContexts[0].Err() != context.Canceled || m.busy || m.cancel != nil || m.opID == old || m.notice != "Operation cancelled" {
		t.Fatal("cancel")
	}
	next, _ = m.Update(resultMsg{kind: opInit, id: old, setupVersion: version, value: scaffold.Result{Destination: "bad", Repositories: []string{"api"}}})
	m = next.(Model)
	if m.notice != "Operation cancelled" || m.setupResult != "" || !m.setupReady {
		t.Fatal("late success")
	}
}

func TestSetupInitIsReadinessGatedAndUsesExactPayload(t *testing.T) {
	f := &fakeOps{}
	m := NewModelWithOperations(true, f)
	m.setupFocus = 6
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd != nil || len(f.inits) != 0 || m.busy || m.notice != "Prerequisites are required" {
		t.Fatal("gate")
	}
	m.setupReady = true
	m.destination.SetValue("workspace")
	next, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd == nil || !m.busy {
		t.Fatal("init start")
	}
	_ = cmd()
	if len(f.inits) != 1 || f.inits[0].destination != "workspace" || !reflect.DeepEqual(f.inits[0].selection, m.selection) {
		t.Fatal("payload")
	}
}

func TestSetupInitCompletionIsSafe(t *testing.T) {
	m := NewModel(true)
	m.busy = true
	m.opID = 7
	next, _ := m.Update(resultMsg{kind: opInit, id: 7, setupVersion: m.setupVersion, value: scaffold.Result{Destination: "workspace", Repositories: []string{"api"}}})
	m = next.(Model)
	if m.busy || m.cancel != nil || !strings.Contains(m.setupResult, "workspace") || !strings.Contains(m.setupResult, "api") {
		t.Fatal("success")
	}
	m.busy = true
	m.opID = 8
	next, _ = m.Update(resultMsg{kind: opInit, id: 8, setupVersion: m.setupVersion, err: errors.New("credential-secret")})
	m = next.(Model)
	if strings.Contains(m.notice, "credential") || !strings.Contains(m.setupResult, "Recovery") {
		t.Fatal("error leak")
	}
	m.busy = true
	m.opID = 9
	m.setupReady = true
	next, _ = m.Update(resultMsg{kind: opInit, id: 9, setupVersion: m.setupVersion, value: "wrong"})
	m = next.(Model)
	if m.setupReady || !strings.Contains(m.setupResult, "Recovery") {
		t.Fatal("type mismatch")
	}
}

func TestSetupInitSuccessSuppressesRepeatUntilSetupChanges(t *testing.T) {
	f := &fakeOps{}
	m := NewModelWithOperations(true, f)
	m.setupReady = true
	m.setupFocus = 6
	next, _ := m.Update(resultMsg{kind: opInit, id: m.opID, setupVersion: m.setupVersion, value: scaffold.Result{Destination: "workspace"}})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd != nil || m.busy || m.notice != "Workspace is already initialized" || len(f.inits) != 0 {
		t.Fatal("repeat")
	}
	m.setupFocus = 0
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "x"}))
	m = next.(Model)
	if m.setupInitialized || m.setupResult != "" || m.setupReady {
		t.Fatal("invalidation")
	}
	m.setupFocus = 6
	next, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd != nil || m.busy || m.notice != "Prerequisites are required" {
		t.Fatal("fresh check gate")
	}
}

func TestSetupInitSuccessRendersAllRepositoriesWithinWidth(t *testing.T) {
	destination := "workspace-" + strings.Repeat("Q", 70)
	repositories := []string{
		"api-" + strings.Repeat("J", 70),
		"web-" + strings.Repeat("K", 70),
	}

	for _, width := range []int{60, 100} {
		for _, noColor := range []bool{true, false} {
			m := NewModel(noColor)
			m.width = width
			m.setupReady = true
			next, _ := m.Update(resultMsg{
				kind:         opInit,
				id:           m.opID,
				setupVersion: m.setupVersion,
				value: scaffold.Result{
					Destination:  destination,
					Repositories: repositories,
				},
			})
			m = next.(Model)
			raw := m.View().Content

			for _, line := range strings.Split(raw, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("noColor=%t width=%d rendered line width=%d", noColor, width, got)
				}
			}

			text := ansi.Strip(raw)
			if noColor {
				normalized := strings.Join(strings.Fields(text), "")
				for _, want := range append([]string{destination}, repositories...) {
					if !strings.Contains(normalized, want) {
						t.Fatalf("noColor=%t width=%d missing %q", noColor, width, want)
					}
				}
				continue
			}

			// Styled cards may place a border between wrapped fragments. The
			// unique markers prove the complete values survived without relying
			// on adjacent text in the rendered frame.
			for _, marker := range []string{"Q", "J", "K"} {
				if got := strings.Count(text, marker); got != 70 {
					t.Fatalf("noColor=%t width=%d marker=%q count=%d", noColor, width, marker, got)
				}
			}
			for _, want := range []string{"Initialized", "workspace-", "api-", "web-"} {
				if !strings.Contains(text, want) {
					t.Fatalf("noColor=%t width=%d missing %q", noColor, width, want)
				}
			}
		}
	}
}

func TestWorkReadyLoadProjectsOnlyPresentationFields(t *testing.T) {
	issues := []beads.Issue{{
		ID:           "smt-123",
		Title:        "Add human review queue",
		Status:       "open",
		Type:         "feature",
		ReviewState:  "queued",
		Labels:       []string{"LABEL-SECRET"},
		Metadata:     []byte(`{"description":"DESCRIPTION-SECRET","token":"METADATA-SECRET"}`),
		Dependencies: []beads.Issue{{ID: "smt-456", Title: "DEPENDENCY-SECRET"}},
	}}
	f := &fakeOps{readyWork: issues}
	m := NewModelWithOperations(true, f)
	m.tab = 1

	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd == nil || !m.busy || m.cancel == nil || m.workState != workLoadLoading || len(f.readyWorkContexts) != 0 {
		t.Fatal("ready work did not start asynchronously")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(f.readyWorkContexts) != 1 || m.busy || m.cancel != nil || m.workState != workLoadLoaded || m.workSelection != 0 || len(m.workItems) != 1 {
		t.Fatal("ready work result state")
	}
	if got, want := m.workItems[0], (workItem{ID: "smt-123", Title: "Add human review queue", Status: "open", Type: "feature", ReviewState: "queued"}); !reflect.DeepEqual(got, want) {
		t.Fatalf("projected item=%+v", got)
	}
	for _, secret := range []string{"LABEL-SECRET", "DESCRIPTION-SECRET", "METADATA-SECRET", "DEPENDENCY-SECRET"} {
		if strings.Contains(fmt.Sprintf("%+v", m.workItems), secret) || strings.Contains(m.View().Content, secret) {
			t.Fatalf("secret retained: %q", secret)
		}
	}
}

func TestWorkReadyResultStatesAreSafe(t *testing.T) {
	cases := []struct {
		name  string
		value any
		err   error
		state workLoadState
		want  string
	}{
		{name: "empty", value: []beads.Issue{}, state: workLoadEmpty, want: "No ready work available"},
		{name: "error wins over value", value: []beads.Issue{{ID: "smt-123"}}, err: errors.New("RAW-ERROR-SECRET"), state: workLoadUnavailable, want: "Ready work is unavailable"},
		{name: "wrong type", value: "wrong", state: workLoadUnavailable, want: "Ready work is unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(true)
			m.tab = 1
			m.busy = true
			m.opID = 7
			m.workItems = []workItem{{ID: "old"}}
			m.workSelection = 3
			next, _ := m.Update(resultMsg{kind: opReadyWork, id: 7, value: tc.value, err: tc.err})
			m = next.(Model)
			if m.busy || m.cancel != nil || m.workState != tc.state || len(m.workItems) != 0 || m.workSelection != 0 || !strings.Contains(m.notice, tc.want) {
				t.Fatalf("state=%q items=%v selection=%d notice=%q", m.workState, m.workItems, m.workSelection, m.notice)
			}
			if strings.Contains(m.notice, "RAW-ERROR-SECRET") || strings.Contains(m.View().Content, "RAW-ERROR-SECRET") {
				t.Fatal("raw adapter error leaked")
			}
			m.notice = ""
			if !strings.Contains(m.View().Content, tc.want) {
				t.Fatalf("state guidance missing: %q", tc.want)
			}
		})
	}
}

func TestWorkReadyEscapeCancelsAndIgnoresLateSuccess(t *testing.T) {
	f := &fakeOps{readyWork: []beads.Issue{{ID: "smt-123", Title: "late success"}}}
	m := NewModelWithOperations(true, f)
	m.tab = 1
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	oldID := m.opID
	lateResult := cmd()
	if len(f.readyWorkContexts) != 1 {
		t.Fatal("ready work context was not captured")
	}
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = next.(Model)
	if f.readyWorkContexts[0].Err() != context.Canceled || m.busy || m.cancel != nil || m.opID == oldID || m.notice != "Operation cancelled" {
		t.Fatal("cancellation state")
	}
	next, _ = m.Update(lateResult)
	m = next.(Model)
	if m.notice != "Operation cancelled" || len(m.workItems) != 0 || m.workState != workLoadLoading {
		t.Fatal("late success overwrote cancellation")
	}
}

func TestWorkRefreshStartsSecondReadyWorkLoad(t *testing.T) {
	f := &fakeOps{readyWork: []beads.Issue{{ID: "one"}, {ID: "two"}}}
	m := NewModelWithOperations(true, f)
	m.tab = 1
	next, first := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	next, _ = m.Update(first())
	m = next.(Model)
	m.workSelection = 1
	if m.workState != workLoadLoaded || len(m.workItems) != 2 || !strings.Contains(m.View().Content, "r refresh") {
		t.Fatal("initial load")
	}
	next, second := m.Update(tea.KeyPressMsg(tea.Key{Text: "r"}))
	m = next.(Model)
	if second == nil || !m.busy || m.workState != workLoadLoading || len(m.workItems) != 0 || m.workSelection != 0 || m.tab != 1 || len(f.readyWorkContexts) != 1 {
		t.Fatal("refresh start")
	}
	_ = second()
	if len(f.readyWorkContexts) != 2 || f.readyWorkContexts[0] == f.readyWorkContexts[1] {
		t.Fatal("refresh adapter calls")
	}
}

func TestWorkQueueInputsInitializeAndRenderLabels(t *testing.T) {
	f := &fakeOps{readyWork: []beads.Issue{{
		ID:           "smt-123",
		Title:        "Selected feature",
		Metadata:     []byte(`{"token":"METADATA-SECRET"}`),
		Labels:       []string{"LABEL-SECRET"},
		Dependencies: []beads.Issue{{Title: "DEPENDENCY-SECRET"}},
	}}}
	m := NewModelWithOperations(true, f)
	if m.workHandoff.Focused() || m.workEvidence.Focused() {
		t.Fatal("queue inputs should start unfocused")
	}
	m.workHandoff.SetValue("docs/handoff.md")
	m.workEvidence.SetValue("evidence/result.md")
	m.tab = 1
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)
	view := m.View().Content
	for _, want := range []string{"Handoff: docs/handoff.md", "Evidence: evidence/result.md"} {
		if !strings.Contains(view, want) {
			t.Fatalf("missing form field %q", want)
		}
	}
	for _, secret := range []string{"METADATA-SECRET", "LABEL-SECRET", "DEPENDENCY-SECRET"} {
		if strings.Contains(view, secret) {
			t.Fatalf("rendered source-only field %q", secret)
		}
	}
}

func TestWorkQueueFocusCyclesThroughInputsAndInertAction(t *testing.T) {
	m := NewModel(true)
	m.tab = 1
	m.workState = workLoadLoaded
	m.workItems = []workItem{{ID: "smt-123"}}
	if m.workFocus != workFocusList || m.workHandoff.Focused() || m.workEvidence.Focused() {
		t.Fatal("initial list focus")
	}

	for _, want := range []workFocus{workFocusHandoff, workFocusEvidence, workFocusQueue, workFocusList, workFocusQueue} {
		key := tea.Key{Code: tea.KeyTab}
		if want == workFocusQueue && m.workFocus == workFocusList {
			key.Mod = tea.ModShift
		}
		next, cmd := m.Update(tea.KeyPressMsg(key))
		m = next.(Model)
		if m.workFocus != want {
			t.Fatalf("focus=%d want=%d", m.workFocus, want)
		}
		if m.workHandoff.Focused() != (want == workFocusHandoff) || m.workEvidence.Focused() != (want == workFocusEvidence) {
			t.Fatalf("focused handoff=%t evidence=%t", m.workHandoff.Focused(), m.workEvidence.Focused())
		}
		if (want == workFocusHandoff || want == workFocusEvidence) != (cmd != nil) {
			t.Fatalf("focus=%d command=%v", want, cmd)
		}
	}
	if !strings.Contains(m.View().Content, "Queue human E2E review") {
		t.Fatal("queue action is not visible")
	}
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd != nil || m.busy || m.tab != 1 || m.workState != workLoadLoaded {
		t.Fatal("queue action is not inert")
	}
}

func TestWorkQueueInputEditingAndSelectionIsolation(t *testing.T) {
	m := NewModel(true)
	m.tab = 1
	m.workState = workLoadLoaded
	m.workItems = []workItem{{ID: "one"}, {ID: "two"}}
	m.workSelection = 1
	m.workEvidence.SetValue("evidence.md")
	m.workEvidence.SetCursor(len("evidence.md"))
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "handoff.md"}))
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	m = next.(Model)
	if m.workHandoff.Value() != "handoff.md" || m.workEvidence.Value() != "evidence.md" || m.workSelection != 1 || m.tab != 1 {
		t.Fatal("handoff editing isolation")
	}
	for _, key := range []tea.Key{{Code: tea.KeyUp}, {Code: tea.KeyDown}, {Text: "j"}, {Text: "k"}} {
		next, _ = m.Update(tea.KeyPressMsg(key))
		m = next.(Model)
	}
	if m.workSelection != 1 || m.tab != 1 {
		t.Fatal("input keys changed selection or tab")
	}
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "proof.md"}))
	m = next.(Model)
	if m.workHandoff.Value() != "handoff.mjkd" || m.workEvidence.Value() != "evidence.mdproof.md" || m.workSelection != 1 || m.tab != 1 {
		t.Fatalf("evidence editing isolation handoff=%q evidence=%q selection=%d tab=%d", m.workHandoff.Value(), m.workEvidence.Value(), m.workSelection, m.tab)
	}
}

func TestWorkQueueInputsIgnoreKeysWhileBusy(t *testing.T) {
	m := NewModel(true)
	m.tab = 1
	m.workState = workLoadLoaded
	m.workItems = []workItem{{ID: "smt-123"}}
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = next.(Model)
	m.workHandoff.SetValue("docs/handoff.md")
	m.busy = true
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "x"}))
	m = next.(Model)
	if cmd != nil || m.workHandoff.Value() != "docs/handoff.md" || m.tab != 1 || m.workFocus != workFocusHandoff {
		t.Fatal("busy input changed state")
	}
}

func TestWorkspaceRelativePath(t *testing.T) {
	cases := []struct {
		name, value, want, guidance string
	}{
		{name: "ordinary relative", value: "docs/handoff.md", want: "docs/handoff.md"},
		{name: "nested spaces trim and clean", value: " docs/feature notes/../handoff file.md ", want: "docs/handoff file.md"},
		{name: "empty", value: " ", guidance: "Evidence path is required"},
		{name: "dot", value: ".", guidance: "Evidence path must name a workspace-relative file"},
		{name: "absolute", value: "/tmp/proof.md", guidance: "Evidence path must be a workspace-relative file"},
		{name: "parent", value: "..", guidance: "Evidence path must be a workspace-relative file"},
		{name: "clean escape", value: "docs/../../outside.md", guidance: "Evidence path must be a workspace-relative file"},
		{name: "nul", value: "evidence/\x00proof.md", guidance: "Evidence path contains control characters"},
		{name: "newline", value: "evidence/\nproof.md", guidance: "Evidence path contains control characters"},
		{name: "tab", value: "evidence/\tproof.md", guidance: "Evidence path contains control characters"},
		{name: "control", value: "evidence/\u0085proof.md", guidance: "Evidence path contains control characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, guidance := workspaceRelativePath("Evidence", tc.value)
			if got != tc.want || guidance != tc.guidance {
				t.Fatalf("path=%q guidance=%q", got, guidance)
			}
		})
	}
}

func TestWorkQueueValidationUsesOnlyLocalState(t *testing.T) {
	cases := []struct {
		name, handoff, evidence, wantHandoff, wantEvidence, wantNotice string
		loaded                                                         bool
		items                                                          []workItem
		selection                                                      int
	}{
		{name: "not loaded", handoff: "docs/handoff.md", evidence: "evidence/proof.md", wantHandoff: "docs/handoff.md", wantEvidence: "evidence/proof.md", wantNotice: "Select ready work before queueing review"},
		{name: "empty list", handoff: "docs/handoff.md", evidence: "evidence/proof.md", wantHandoff: "docs/handoff.md", wantEvidence: "evidence/proof.md", wantNotice: "Select ready work before queueing review", loaded: true},
		{name: "out of range", handoff: "docs/handoff.md", evidence: "evidence/proof.md", wantHandoff: "docs/handoff.md", wantEvidence: "evidence/proof.md", wantNotice: "Select ready work before queueing review", loaded: true, items: []workItem{{ID: "smt-123"}}, selection: 1},
		{name: "blank original id", handoff: "docs/handoff.md", evidence: "evidence/proof.md", wantHandoff: "docs/handoff.md", wantEvidence: "evidence/proof.md", wantNotice: "Select ready work before queueing review", loaded: true, items: []workItem{{ID: " "}}},
		{name: "invalid handoff", handoff: "../handoff.md", evidence: "evidence/proof.md", wantHandoff: "../handoff.md", wantEvidence: "evidence/proof.md", wantNotice: "Handoff path must be a workspace-relative file", loaded: true, items: []workItem{{ID: "smt-123"}}},
		{name: "invalid evidence", handoff: "docs/handoff.md", evidence: ".", wantHandoff: "docs/handoff.md", wantEvidence: ".", wantNotice: "Evidence path must name a workspace-relative file", loaded: true, items: []workItem{{ID: "smt-123"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeOps{}
			m := NewModelWithOperations(true, f)
			m.tab = 1
			m.workFocus = workFocusQueue
			m.workSelection = tc.selection
			m.workHandoff.SetValue(tc.handoff)
			m.workEvidence.SetValue(tc.evidence)
			if tc.loaded {
				m.workState = workLoadLoaded
			}
			m.workItems = tc.items
			next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
			m = next.(Model)
			if cmd != nil || m.notice != tc.wantNotice || m.workHandoff.Value() != tc.wantHandoff || m.workEvidence.Value() != tc.wantEvidence || len(f.queueReviews) != 0 || len(f.readyWorkContexts) != 0 {
				t.Fatalf("notice=%q handoff=%q evidence=%q queue=%d ready=%d", m.notice, m.workHandoff.Value(), m.workEvidence.Value(), len(f.queueReviews), len(f.readyWorkContexts))
			}
		})
	}
}

func TestWorkQueueStartsTypedCommandWithCapturedPayload(t *testing.T) {
	f := &fakeOps{queueResult: beads.QueueResult{FeatureID: "smt-123", ReviewID: "smt-456"}}
	m := NewModelWithOperations(true, f)
	m.tab = 1
	m.workFocus = workFocusQueue
	m.workState = workLoadLoaded
	m.workItems = []workItem{{ID: "smt-123"}}
	m.workHandoff.SetValue(" docs/feature/../handoff.md ")
	m.workEvidence.SetValue(" evidence/./proof.md ")
	m.queueOutcome = queueOutcome{FeatureID: "old queue result"}
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd == nil || !m.busy || m.cancel == nil || m.notice != "Queueing human E2E review" || m.queueOutcome != (queueOutcome{}) || len(f.queueReviews) != 0 {
		t.Fatal("queue command start")
	}
	m.workItems[0].ID = "changed"
	m.workHandoff.SetValue("changed/handoff.md")
	m.workEvidence.SetValue("changed/evidence.md")
	_ = cmd()
	if len(f.queueReviews) != 1 || len(f.queueContexts) != 1 {
		t.Fatal("queue adapter call")
	}
	if got := f.queueReviews[0]; got.featureID != "smt-123" || got.handoff != "docs/handoff.md" || got.evidence != "evidence/proof.md" {
		t.Fatalf("captured payload=%+v", got)
	}
}

func TestWorkQueueCompletionProjectsSafeOutcome(t *testing.T) {
	m := NewModel(true)
	m.tab = 1
	m.busy = true
	m.opID = 11
	next, _ := m.Update(resultMsg{kind: opQueueReview, id: 11, value: beads.QueueResult{FeatureID: "smt-123", ReviewID: "smt-456", Recovery: "inspect review"}})
	m = next.(Model)
	if m.busy || m.cancel != nil || m.notice != "Human E2E review queued" || m.queueOutcome != (queueOutcome{FeatureID: "smt-123", ReviewID: "smt-456", Recovery: "inspect review"}) {
		t.Fatalf("outcome=%+v notice=%q", m.queueOutcome, m.notice)
	}
	for _, want := range []string{"Feature: smt-123", "Human review: smt-456", "Recovery: inspect review"} {
		if !strings.Contains(m.View().Content, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestWorkQueueCompletionFailureAndInvalidResultsAreSafe(t *testing.T) {
	t.Run("wrong type", func(t *testing.T) {
		m := NewModel(true)
		m.busy = true
		m.opID = 12
		m.queueOutcome = queueOutcome{FeatureID: "old"}
		next, _ := m.Update(resultMsg{kind: opQueueReview, id: 12, value: "wrong"})
		m = next.(Model)
		if m.busy || m.cancel != nil || m.notice != "Review queueing result was invalid" || m.queueOutcome != (queueOutcome{}) {
			t.Fatal("wrong result handling")
		}
	})
	t.Run("error partial", func(t *testing.T) {
		m := NewModel(true)
		m.tab = 1
		m.busy = true
		m.opID = 13
		next, _ := m.Update(resultMsg{kind: opQueueReview, id: 13, err: errors.New("RAW-ERROR-SECRET"), value: beads.QueueResult{FeatureID: "smt-\x1b[31m123", ReviewID: "review-\xff", Recovery: "retry\nwith\tcare"}})
		m = next.(Model)
		if m.busy || m.cancel != nil || m.notice != "Review queueing failed safely" {
			t.Fatal("error result handling")
		}
		stored := fmt.Sprintf("%+v", m.queueOutcome)
		if strings.Contains(stored, "\x1b") || strings.Contains(stored, "\xff") || strings.Contains(stored, "\n") || strings.Contains(stored, "\t") {
			t.Fatalf("unsafe outcome=%q", stored)
		}
		view := m.View().Content
		if strings.Contains(view, "RAW-ERROR-SECRET") || strings.Contains(view, "\x1b") || strings.Contains(view, "\xff") {
			t.Fatalf("unsafe view=%q", view)
		}
		for _, want := range []string{"Feature: smt-123", "Human review: review-", "Recovery: retry with care"} {
			if !strings.Contains(view, want) {
				t.Fatalf("missing %q", want)
			}
		}
	})
}

func TestWorkQueueOutcomeRendererPreservesSafeLongFieldsWithinWidth(t *testing.T) {
	feature := "feature-" + strings.Repeat("X", 81) + "\x1b[31m"
	review := "review-" + strings.Repeat("J", 81) + "\xff"
	recovery := "recovery-" + strings.Repeat("Z", 81) + "\nretry"
	for _, width := range []int{60, 100} {
		for _, noColor := range []bool{true, false} {
			m := NewModel(noColor)
			m.tab = 1
			m.width = width
			next, _ := m.Update(resultMsg{kind: opReadyWork, id: m.opID, value: []beads.Issue{{ID: "smt-123", Title: "safe title", Metadata: []byte(`{"token":"METADATA-SECRET"}`)}}})
			m = next.(Model)
			m.busy = true
			m.opID = 23
			next, _ = m.Update(resultMsg{kind: opQueueReview, id: 23, err: errors.New("RAW-ERROR-SECRET"), value: beads.QueueResult{FeatureID: feature, ReviewID: review, Recovery: recovery}})
			m = next.(Model)
			raw := m.View().Content
			if noColor && strings.Contains(raw, "\x1b") {
				t.Fatal("plain ANSI")
			}
			if !noColor && !strings.Contains(raw, "\x1b") {
				t.Fatal("styled ANSI")
			}
			for _, line := range strings.Split(raw, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("noColor=%t width=%d line width=%d", noColor, width, got)
				}
			}
			text := ansi.Strip(raw)
			for _, secret := range []string{"RAW-ERROR-SECRET", "METADATA-SECRET", "\xff", "\nretry"} {
				if strings.Contains(text, secret) {
					t.Fatalf("noColor=%t rendered unsafe value %q", noColor, secret)
				}
			}
			for _, want := range []string{"Feature: feature-", "Human review: review-", "Recovery: recovery-"} {
				if !strings.Contains(text, want) {
					t.Fatalf("noColor=%t missing %q", noColor, want)
				}
			}
			if noColor {
				normalized := strings.Join(strings.Fields(text), "")
				for _, want := range []string{"feature-" + strings.Repeat("X", 81), "review-" + strings.Repeat("J", 81), "recovery-" + strings.Repeat("Z", 81)} {
					if !strings.Contains(normalized, want) {
						t.Fatalf("missing %q", want[:8])
					}
				}
			} else {
				for _, marker := range []string{"X", "J", "Z"} {
					if got := strings.Count(text, marker); got != 81 {
						t.Fatalf("marker=%q count=%d", marker, got)
					}
				}
			}
		}
	}
}

func TestWorkQueueEscapeCancelsAndIgnoresLateSuccess(t *testing.T) {
	f := &fakeOps{queueResult: beads.QueueResult{FeatureID: "smt-123", ReviewID: "smt-456"}}
	m := NewModelWithOperations(true, f)
	m.tab = 1
	m.workFocus = workFocusQueue
	m.workState = workLoadLoaded
	m.workItems = []workItem{{ID: "smt-123"}}
	m.workHandoff.SetValue("docs/handoff.md")
	m.workEvidence.SetValue("evidence/proof.md")
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	oldID := m.opID
	lateSuccess := cmd()
	if len(f.queueContexts) != 1 {
		t.Fatal("queue context was not captured")
	}
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = next.(Model)
	if f.queueContexts[0].Err() != context.Canceled || m.busy || m.cancel != nil || m.opID == oldID || m.notice != "Operation cancelled" {
		t.Fatal("queue cancellation state")
	}
	next, _ = m.Update(lateSuccess)
	m = next.(Model)
	if m.notice != "Operation cancelled" || m.queueOutcome != (queueOutcome{}) || m.workState != workLoadLoaded || len(m.workItems) != 1 || m.workItems[0].ID != "smt-123" {
		t.Fatal("late queue success overwrote state")
	}
}

func TestHumanReviewsLoadProjectsOnlyPresentationFields(t *testing.T) {
	source := beads.Issue{
		ID:          "review-123",
		Title:       "Human E2E review",
		Status:      "open",
		Type:        "task",
		ReviewState: "queued",
		Parent:      "PARENT-SECRET",
		Labels:      []string{"LABEL-SECRET"},
		Metadata:    []byte(`{"description":"DESCRIPTION-SECRET","token":"METADATA-SECRET"}`),
		Dependencies: []beads.Issue{{
			ID:    "dependency-1",
			Title: "DEPENDENCY-SECRET",
		}},
	}
	f := &fakeOps{reviews: []beads.Issue{source}}
	m := NewModelWithOperations(true, f)
	m.tab = 2
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd == nil || !m.busy || m.cancel == nil || m.reviewLoad != reviewLoadLoading || len(f.reviewContexts) != 0 {
		t.Fatal("review load start")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(f.reviewContexts) != 1 || m.busy || m.cancel != nil || m.reviewLoad != reviewLoadLoaded || len(m.reviewItems) != 1 {
		t.Fatal("review load state")
	}
	if got, want := m.reviewItems[0], (reviewItem{ID: source.ID, Title: source.Title, Status: source.Status, Type: source.Type, ReviewState: source.ReviewState}); !reflect.DeepEqual(got, want) {
		t.Fatalf("review item=%+v", got)
	}
	for _, secret := range []string{"PARENT-SECRET", "LABEL-SECRET", "DESCRIPTION-SECRET", "METADATA-SECRET", "DEPENDENCY-SECRET"} {
		if strings.Contains(fmt.Sprintf("%+v", m.reviewItems), secret) || strings.Contains(m.View().Content, secret) {
			t.Fatalf("secret retained: %q", secret)
		}
	}
}

func TestHumanReviewsResultStatesAreSafe(t *testing.T) {
	cases := []struct {
		name  string
		value any
		err   error
		state reviewLoadState
		want  string
	}{
		{name: "empty", value: []beads.Issue{}, state: reviewLoadEmpty, want: "No human E2E reviews available"},
		{name: "error", value: []beads.Issue{{ID: "review-123"}}, err: errors.New("RAW-REVIEW-ERROR"), state: reviewLoadUnavailable, want: "Human reviews are unavailable"},
		{name: "wrong type", value: "wrong", state: reviewLoadUnavailable, want: "Human reviews are unavailable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewModel(true)
			m.tab = 2
			m.busy = true
			m.opID = 31
			m.reviewItems = []reviewItem{{ID: "old"}}
			next, _ := m.Update(resultMsg{kind: opListReviews, id: 31, value: tc.value, err: tc.err})
			m = next.(Model)
			if m.busy || m.cancel != nil || m.reviewLoad != tc.state || len(m.reviewItems) != 0 || !strings.Contains(m.notice, tc.want) || strings.Contains(m.notice, "RAW-REVIEW-ERROR") {
				t.Fatalf("state=%q items=%v notice=%q", m.reviewLoad, m.reviewItems, m.notice)
			}
			if strings.Contains(m.View().Content, "RAW-REVIEW-ERROR") {
				t.Fatal("raw error rendered")
			}
		})
	}
}

func TestHumanReviewRendererSelectionAndRefresh(t *testing.T) {
	long := reviewItem{ID: "id-" + strings.Repeat("X", 70) + "\x1b[31m", Title: "title-" + strings.Repeat("J", 70) + "\nnext", Status: "open", Type: "task", ReviewState: "retest-queued"}
	for _, width := range []int{60, 100} {
		for _, noColor := range []bool{true, false} {
			m := NewModel(noColor)
			m.tab = 2
			m.width = width
			m.reviewLoad = reviewLoadLoaded
			m.reviewItems = []reviewItem{long, {ID: "two", ReviewState: "failed"}}
			raw := m.View().Content
			if noColor && strings.Contains(raw, "\x1b") {
				t.Fatal("plain ANSI")
			}
			for _, line := range strings.Split(raw, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("width=%d", width)
				}
			}
			text := ansi.Strip(raw)
			for _, want := range []string{"ID:", "Title:", "Status:", "Type:", "Review state:", "retest-queued", "failed"} {
				if !strings.Contains(text, want) {
					t.Fatal(want)
				}
			}
			if strings.Contains(text, "\x1b") {
				t.Fatal("control")
			}
		}
	}
	f := &fakeOps{reviews: []beads.Issue{{ID: "one"}, {ID: "two"}}}
	m := NewModelWithOperations(true, f)
	m.tab = 2
	next, first := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	next, _ = m.Update(first())
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	m = next.(Model)
	if m.reviewSelection != 1 {
		t.Fatal("selection")
	}
	next, second := m.Update(tea.KeyPressMsg(tea.Key{Text: "r"}))
	m = next.(Model)
	if second == nil || !m.busy || m.reviewSelection != 0 || len(m.reviewItems) != 0 || len(f.reviewContexts) != 1 {
		t.Fatal("refresh start")
	}
	_ = second()
	if len(f.reviewContexts) != 2 {
		t.Fatal("refresh call")
	}
}

func TestHumanReviewsEscapeCancelsAndIgnoresLateResult(t *testing.T) {
	f := &fakeOps{reviews: []beads.Issue{{ID: "review-123", Title: "late"}}}
	m := NewModelWithOperations(true, f)
	m.tab = 2
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	oldID := m.opID
	lateResult := cmd()
	if len(f.reviewContexts) != 1 {
		t.Fatal("review context was not captured")
	}
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = next.(Model)
	if f.reviewContexts[0].Err() != context.Canceled || m.busy || m.cancel != nil || m.opID == oldID || m.notice != "Operation cancelled" || m.reviewLoad != reviewLoadLoading || len(m.reviewItems) != 0 {
		t.Fatal("review cancellation state")
	}
	next, _ = m.Update(lateResult)
	m = next.(Model)
	if m.notice != "Operation cancelled" || m.reviewLoad != reviewLoadLoading || len(m.reviewItems) != 0 {
		t.Fatal("late review result overwrote state")
	}
}

func TestHumanPassFormOpenGuardsAndClose(t *testing.T) {
	m := NewModel(true)
	m.tab = 2
	m.reviewLoad = reviewLoadLoaded
	m.reviewItems = []reviewItem{{ID: "review-1", ReviewState: "queued"}}
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "p"}))
	m = next.(Model)
	if !m.passForm || !strings.Contains(m.View().Content, "Pass review: review-1 (queued)") {
		t.Fatal("open")
	}
	m.passReviewer.SetValue("Ada")
	m.passEvidence.SetValue("proof")
	m.passResult = "old"
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	m = next.(Model)
	if m.passForm || m.passReviewer.Value() != "" || m.passEvidence.Value() != "" || m.passResult != "" {
		t.Fatal("close")
	}
	for _, state := range []string{"failed", ""} {
		n := NewModel(true)
		n.tab = 2
		n.reviewLoad = reviewLoadLoaded
		n.reviewItems = []reviewItem{{ID: "x", ReviewState: state}}
		next, _ := n.Update(tea.KeyPressMsg(tea.Key{Text: "p"}))
		if next.(Model).passForm {
			t.Fatal("guard")
		}
	}
}

func TestHumanPassReviewerOwnsInputKeys(t *testing.T) {
	m := NewModel(true)
	m.tab = 2
	m.reviewLoad = reviewLoadLoaded
	m.reviewItems = []reviewItem{{ID: "r", ReviewState: "queued"}}
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "p"}))
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "hello"}))
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	m = next.(Model)
	if m.passReviewer.Value() != "hello" || m.passEvidence.Value() != "" || m.tab != 2 || m.reviewSelection != 0 {
		t.Fatal("reviewer ownership")
	}
}

func TestHumanPassEvidenceOwnsInputKeysAndBusyLocksBothInputs(t *testing.T) {
	m := NewModel(true)
	m.tab = 2
	m.reviewLoad = reviewLoadLoaded
	m.reviewItems = []reviewItem{{ID: "r", ReviewState: "queued"}}
	next, _ := m.Update(tea.KeyPressMsg(tea.Key{Text: "p"}))
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Text: "proof"}))
	m = next.(Model)
	next, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	m = next.(Model)
	if m.passEvidence.Value() != "proof" || m.passReviewer.Value() != "" || m.tab != 2 || m.reviewSelection != 0 {
		t.Fatal("evidence ownership")
	}
	m.passReviewer.SetValue("Ada")
	m.busy = true
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "x"}))
	m = next.(Model)
	if cmd != nil || m.passReviewer.Value() != "Ada" || m.passEvidence.Value() != "proof" {
		t.Fatal("busy lock")
	}
}

func TestWorkRendererSanitizesDisplayWithoutMutatingStoredItems(t *testing.T) {
	if got, want := displayWorkField("a\nb\tc\x01d\u0085e\x7ff\x9fg"), "a b c d e f g"; got != want {
		t.Fatalf("sanitized field=%q", got)
	}
	if got, want := displayWorkField("a\x1b[31mb"), "ab"; got != want {
		t.Fatalf("ANSI suffix field=%q", got)
	}
	item := workItem{
		ID:          "id-" + strings.Repeat("X", 70) + "\x1b[31m",
		Title:       "title-" + strings.Repeat("J", 70) + "\nnext\t\x01\u0085",
		Status:      "status-" + strings.Repeat("K", 70) + "\rstate",
		Type:        "type-" + strings.Repeat("V", 70) + "\x7fkind",
		ReviewState: "review-" + strings.Repeat("Z", 70) + "\x9fstate",
	}
	for _, width := range []int{60, 100} {
		for _, noColor := range []bool{true, false} {
			m := NewModel(noColor)
			m.tab = 1
			m.width = width
			m.workState = workLoadLoaded
			m.workItems = []workItem{item}
			raw := m.View().Content
			if !reflect.DeepEqual(m.workItems[0], item) {
				t.Fatal("renderer mutated stored item")
			}
			if noColor && strings.Contains(raw, "\x1b") {
				t.Fatal("plain mode contains ANSI")
			}
			if !noColor && !strings.Contains(raw, "\x1b") {
				t.Fatal("styled mode lacks ANSI")
			}
			for _, line := range strings.Split(raw, "\n") {
				if got := lipgloss.Width(line); got > width {
					t.Fatalf("noColor=%t width=%d line width=%d", noColor, width, got)
				}
			}
			text := ansi.Strip(raw)
			for _, control := range []string{"\x1b", "\t", "\x01", "\u0085", "\x7f", "\x9f"} {
				if strings.Contains(text, control) {
					t.Fatalf("noColor=%t rendered control %q", noColor, control)
				}
			}
			for _, want := range []string{"> ID:", "Title:", "Status:", "Type:", "Review state:", "id-", "title-", "status-", "type-", "review-"} {
				if !strings.Contains(text, want) {
					t.Fatalf("noColor=%t missing %q", noColor, want)
				}
			}
			for _, marker := range []string{"X", "J", "K", "V", "Z"} {
				if got := strings.Count(text, marker); got != 70 {
					t.Fatalf("noColor=%t marker=%q count=%d", noColor, marker, got)
				}
			}
		}
	}
}

func TestWorkRendererShowsLoadActionAndLoadingState(t *testing.T) {
	m := NewModel(true)
	m.tab = 1
	if !strings.Contains(m.View().Content, "Press enter to load ready work") {
		t.Fatal("load action")
	}
	m.workState = workLoadLoading
	if !strings.Contains(m.View().Content, "Loading ready work") {
		t.Fatal("loading state")
	}
}

func TestWorkRendererExcludesSourceOnlyFields(t *testing.T) {
	f := &fakeOps{readyWork: []beads.Issue{{
		ID:           "smt-123",
		Title:        "Visible title",
		Status:       "open",
		Type:         "feature",
		ReviewState:  "queued",
		Labels:       []string{"LABEL-SECRET"},
		Metadata:     []byte(`{"description":"DESCRIPTION-SECRET","token":"METADATA-SECRET"}`),
		Dependencies: []beads.Issue{{ID: "smt-456", Title: "DEPENDENCY-SECRET"}},
	}}}
	m := NewModelWithOperations(true, f)
	m.tab = 1
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	next, _ = m.Update(cmd())
	m = next.(Model)
	for _, secret := range []string{"LABEL-SECRET", "DESCRIPTION-SECRET", "METADATA-SECRET", "DEPENDENCY-SECRET"} {
		if strings.Contains(m.View().Content, secret) {
			t.Fatalf("rendered source-only field: %q", secret)
		}
	}
}

func TestWorkSelectionIsClampedAndStaysOnWorkTab(t *testing.T) {
	m := NewModel(true)
	m.tab = 1
	m.workState = workLoadLoaded
	m.workItems = []workItem{{ID: "one"}, {ID: "two"}, {ID: "three"}}

	for _, key := range []tea.Key{{Code: tea.KeyUp}, {Text: "j"}} {
		next, _ := m.Update(tea.KeyPressMsg(key))
		m = next.(Model)
		if m.workSelection != 0 || m.tab != 1 {
			t.Fatalf("key=%q moved above first item", key.String())
		}
	}
	for _, key := range []tea.Key{{Code: tea.KeyDown}, {Text: "k"}, {Code: tea.KeyDown}} {
		next, _ := m.Update(tea.KeyPressMsg(key))
		m = next.(Model)
	}
	if m.workSelection != 2 || m.tab != 1 {
		t.Fatalf("selection=%d tab=%d", m.workSelection, m.tab)
	}
}

func TestStaleInitCompletionCannotClaimSuccess(t *testing.T) {
	m := NewModel(true)
	m.notice = "safe"
	m.setupResult = "safe"
	m.busy = true
	m.opID = 5
	m.setupVersion = 2
	next, _ := m.Update(resultMsg{kind: opInit, id: 5, setupVersion: 1, value: scaffold.Result{Destination: "bad"}})
	m = next.(Model)
	if m.notice != "safe" || m.setupResult != "safe" || m.busy {
		t.Fatal("stale version")
	}
	m.busy = true
	m.opID = 6
	next, _ = m.Update(resultMsg{kind: opInit, id: 5, setupVersion: 2, value: scaffold.Result{Destination: "bad"}})
	m = next.(Model)
	if m.notice != "safe" || m.setupResult != "safe" || !m.busy {
		t.Fatal("stale id")
	}
}
func (f *fakeOps) QueueReview(ctx context.Context, featureID, handoff, evidence string) (beads.QueueResult, error) {
	f.queueContexts = append(f.queueContexts, ctx)
	f.queueReviews = append(f.queueReviews, struct{ featureID, handoff, evidence string }{featureID, handoff, evidence})
	return f.queueResult, f.queueErr
}
func (f *fakeOps) HumanPass(context.Context, string, string, string) (beads.Recovery, error) {
	return beads.Recovery{}, nil
}
func (f *fakeOps) HumanFail(context.Context, string, beads.FailureInput) (beads.Recovery, error) {
	return beads.Recovery{}, nil
}
func (f *fakeOps) RequeueAfterFix(context.Context, string) (beads.Recovery, error) {
	return beads.Recovery{}, nil
}
func (f *fakeOps) ReleaseReadiness(context.Context) (beads.ReleaseReadiness, error) {
	return beads.ReleaseReadiness{}, nil
}

func TestSetupCheckUsesSelectionAndResult(t *testing.T) {
	f := &fakeOps{result: prereq.Result{Findings: []prereq.Finding{{ID: "x", Status: prereq.StatusReady}}}}
	m := NewModelWithOperations(true, f)
	m.selection.Web = false
	m.setupFocus = 5
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if len(f.checks) != 0 || !m.busy {
		t.Fatal("async start")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if len(f.checks) != 1 || m.setupReady != true {
		t.Fatal("check result")
	}
}

func TestSetupCheckRequiresAComponent(t *testing.T) {
	f := &fakeOps{}
	m := NewModelWithOperations(true, f)
	m.selection = scaffold.Selection{Codex: true}
	m.setupFocus = 5
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd != nil || len(f.checks) != 0 || m.busy || m.notice != "Select at least one component" {
		t.Fatalf("cmd=%v calls=%d busy=%t notice=%q", cmd, len(f.checks), m.busy, m.notice)
	}
}

func TestSetupCheckStartClearsPriorState(t *testing.T) {
	f := &fakeOps{}
	m := NewModelWithOperations(true, f)
	m.setupFocus = 5
	m.setupReady = true
	m.findings = []prereq.Finding{{ID: "old"}}
	m.setupResult = "old"
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd == nil || !m.busy || m.setupReady || len(m.findings) != 0 || m.setupResult != "" {
		t.Fatalf("cmd=%v busy=%t ready=%t findings=%v result=%q", cmd, m.busy, m.setupReady, m.findings, m.setupResult)
	}
}

func TestSetupCheckMissingAndMalformedRemainNotReady(t *testing.T) {
	for _, status := range []prereq.Status{prereq.StatusMissing, prereq.StatusMalformed} {
		t.Run(string(status), func(t *testing.T) {
			f := &fakeOps{result: prereq.Result{Findings: []prereq.Finding{{ID: "tool", Status: status, Message: "safe"}}}}
			m := NewModelWithOperations(true, f)
			m.setupFocus = 5
			next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
			m = next.(Model)
			next, _ = m.Update(cmd())
			m = next.(Model)
			if m.busy || m.setupReady || len(m.findings) != 1 || m.notice == "" || strings.Contains(m.notice, "safe") {
				t.Fatalf("busy=%t ready=%t findings=%v notice=%q", m.busy, m.setupReady, m.findings, m.notice)
			}
		})
	}
}

func TestSetupRecheckUsesCurrentRequirementsExactlyTwice(t *testing.T) {
	f := &fakeOps{result: prereq.Result{Findings: []prereq.Finding{{ID: "ok", Status: prereq.StatusReady}}}}
	m := NewModelWithOperations(true, f)
	m.selection = scaffold.Selection{API: true, Database: true, DevOps: true, Codex: true}
	m.setupFocus = 5
	for range 2 {
		next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
		m = next.(Model)
		next, _ = m.Update(cmd())
		m = next.(Model)
	}
	if len(f.checks) != 2 || !reflect.DeepEqual(f.checks[0], scaffold.Requirements(m.selection)) || !reflect.DeepEqual(f.checks[1], scaffold.Requirements(m.selection)) {
		t.Fatalf("checks=%#v", f.checks)
	}
}

func TestSetupCheckStaleVersionUnlocksWithoutRestoringReady(t *testing.T) {
	f := &fakeOps{}
	m := NewModelWithOperations(true, f)
	m.setupFocus = 5
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	_ = cmd
	oldID, oldVersion := m.opID, m.setupVersion
	m.invalidateSetup()
	next, _ = m.Update(resultMsg{value: prereq.Result{Findings: []prereq.Finding{{ID: "ok", Status: prereq.StatusReady}}}, id: oldID, setupVersion: oldVersion, kind: opCheck})
	m = next.(Model)
	if m.busy || m.cancel != nil || m.setupReady || len(m.findings) != 0 || m.notice != "Setup changed; re-check prerequisites" {
		t.Fatalf("busy=%t cancel=%v ready=%t findings=%v notice=%q", m.busy, m.cancel, m.setupReady, m.findings, m.notice)
	}
}

func TestSetupCheckErrorAndWrongTypeClearSafely(t *testing.T) {
	for _, message := range []resultMsg{{err: errors.New("credential-secret"), kind: opCheck}, {value: "wrong", kind: opCheck}} {
		t.Run("case", func(t *testing.T) {
			m := NewModel(true)
			m.busy = true
			m.setupReady = true
			m.findings = []prereq.Finding{{ID: "old"}}
			message.id, message.setupVersion = m.opID, m.setupVersion
			next, _ := m.Update(message)
			m = next.(Model)
			if m.busy || m.cancel != nil || m.setupReady || len(m.findings) != 0 || m.notice == "" || strings.Contains(m.notice, "credential-secret") {
				t.Fatalf("busy=%t ready=%t findings=%v notice=%q", m.busy, m.setupReady, m.findings, m.notice)
			}
		})
	}
}

func TestOpNonePreservesSetupState(t *testing.T) {
	for _, message := range []resultMsg{{kind: opNone}, {kind: opNone, err: errors.New("safe")}} {
		m := NewModel(true)
		m.setupReady = true
		m.findings = []prereq.Finding{{ID: "keep"}}
		m.setupResult, m.notice = "keep", "before"
		message.id, message.setupVersion = m.opID, m.setupVersion
		next, _ := m.Update(message)
		m = next.(Model)
		if !m.setupReady || len(m.findings) != 1 || m.setupResult != "keep" {
			t.Fatalf("ready=%t findings=%v result=%q", m.setupReady, m.findings, m.setupResult)
		}
	}
}

func TestBusyCheckIgnoresFormInputButAcceptsResize(t *testing.T) {
	f := &fakeOps{}
	m := NewModelWithOperations(true, f)
	m.setupFocus = 5
	next, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	m = next.(Model)
	if cmd == nil || !m.busy {
		t.Fatal("check did not start")
	}
	focus, tab, destination, selection := m.setupFocus, m.tab, m.destination.Value(), m.selection
	for _, key := range []tea.Key{{Code: tea.KeyTab}, {Code: tea.KeyRight}, {Text: "x"}, {Code: tea.KeyEnter}} {
		next, extra := m.Update(tea.KeyPressMsg(key))
		m = next.(Model)
		if extra != nil {
			t.Fatal("extra command")
		}
	}
	if m.setupFocus != focus || m.tab != tab || m.destination.Value() != destination || !reflect.DeepEqual(m.selection, selection) || len(f.checks) != 0 {
		t.Fatal("busy input mutated model")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 77, Height: 22})
	m = next.(Model)
	if m.width != 77 || m.height != 22 {
		t.Fatal("resize blocked")
	}
}

func TestCompactNavigationAndRenderedWidths(t *testing.T) {
	for _, noColor := range []bool{true, false} {
		m := NewModel(noColor)
		m.width = 60
		view := m.View().Content
		if !noColor {
			view = ansi.Strip(view)
		}
		if !strings.Contains(view, "> Setup\n  Work") {
			t.Fatalf("compact nav=%q", view)
		}
	}
	for _, width := range []int{60, 100} {
		for _, noColor := range []bool{true, false} {
			m := NewModel(noColor)
			m.width = width
			for _, line := range strings.Split(m.View().Content, "\n") {
				if lipgloss.Width(line) > width {
					t.Fatalf("width=%d line=%q", width, line)
				}
			}
		}
	}
}
