package tui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/parmcoder/smt/internal/beads"
)

type fakeLoader struct {
	items []beads.Issue
	err   error
	calls int
}

type fakeAppRunner struct {
	calls int
	got   tea.Model
	err   error
}

func (f *fakeAppRunner) Run(m tea.Model, _ ...tea.ProgramOption) error {
	f.calls++
	f.got = m
	return f.err
}

func TestRunLocalUsesReviewOnlyRunner(t *testing.T) {
	oldRunner, oldService := Runner, newService
	t.Cleanup(func() { Runner, newService = oldRunner, oldService })
	runner := &fakeAppRunner{}
	Runner = runner
	newService = func(string) loader { return &fakeLoader{} }
	if err := RunLocal(context.Background(), true, "/workspace"); err != nil || runner.calls != 1 {
		t.Fatalf("err=%v calls=%d", err, runner.calls)
	}
	if got, ok := runner.got.(model); !ok || !got.noColor {
		t.Fatalf("model=%T %+v", runner.got, runner.got)
	}
}

func (f *fakeLoader) ListReviews(context.Context) ([]beads.Issue, error) {
	f.calls++
	return f.items, f.err
}

func TestReviewModelLoadsRefreshesNavigatesAndQuits(t *testing.T) {
	f := &fakeLoader{items: []beads.Issue{{ID: "one", Title: "First", Status: "open", Type: "task", ReviewState: "queued"}, {ID: "two", Title: "Second", Status: "open", Type: "task", ReviewState: "failed"}}}
	m := model{ctx: context.Background(), noColor: true, service: f}
	n, cmd := m.Update(tea.KeyPressMsg(tea.Key{Text: "r"}))
	m = n.(model)
	next := cmd()
	n, _ = m.Update(next)
	m = n.(model)
	if f.calls != 1 || len(m.items) != 2 {
		t.Fatalf("calls=%d items=%+v", f.calls, m.items)
	}
	for _, key := range []tea.KeyPressMsg{tea.KeyPressMsg(tea.Key{Text: "j"}), tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}), tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}), tea.KeyPressMsg(tea.Key{Text: "k"})} {
		n, _ := m.Update(key)
		m = n.(model)
	}
	if m.selected != 0 {
		t.Fatalf("selection=%d", m.selected)
	}
	_, quit := m.Update(tea.KeyPressMsg(tea.Key{Text: "q"}))
	if quit == nil {
		t.Fatal("quit command absent")
	}
}

func TestReviewModelRendersSafeDTOAndPlainErrors(t *testing.T) {
	f := &fakeLoader{items: []beads.Issue{{ID: "review-1", Title: "Visible title", Status: "open", Type: "task", ReviewState: "queued", Labels: []string{"SECRET-LABEL"}, Parent: "SECRET-PARENT", Dependencies: []beads.Issue{{ID: "SECRET-DEP"}}}}}
	m := model{ctx: context.Background(), noColor: true, service: f}
	msg := m.Init()()
	n, _ := m.Update(msg)
	m = n.(model)
	view := m.View().Content
	for _, want := range []string{"review-1", "Visible title", "open", "task", "queued"} {
		if !strings.Contains(view, want) {
			t.Fatalf("view missing %q: %q", want, view)
		}
	}
	for _, secret := range []string{"SECRET-LABEL", "SECRET-PARENT", "SECRET-DEP", "\x1b"} {
		if strings.Contains(view, secret) {
			t.Fatalf("unsafe rendering %q in %q", secret, view)
		}
	}
	f.err = errors.New("RAW-TOKEN")
	msg = m.Init()()
	n, _ = m.Update(msg)
	m = n.(model)
	if strings.Contains(m.View().Content, "RAW-TOKEN") {
		t.Fatalf("raw error rendered: %q", m.View().Content)
	}
}
