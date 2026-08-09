// Package tui is SMT's deliberately small review-list terminal UI.
package tui

import (
	tea "charm.land/bubbletea/v2"
	"context"
	"fmt"
	"github.com/parmcoder/smt/internal/beads"
	"strings"
)

type AppRunner interface {
	Run(tea.Model, ...tea.ProgramOption) error
}
type app struct{}

func (app) Run(m tea.Model, o ...tea.ProgramOption) error {
	_, e := tea.NewProgram(m, o...).Run()
	return e
}

var Runner AppRunner = app{}
var newService = func(root string) loader { return beads.New(root, beads.CommandRunner{}) }

type loader interface {
	ListReviews(context.Context) ([]beads.Issue, error)
}
type reviewItem struct{ ID, Title, Status, Type, ReviewState string }
type model struct {
	ctx      context.Context
	noColor  bool
	service  loader
	items    []reviewItem
	err      error
	selected int
}
type loaded struct {
	items []reviewItem
	err   error
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		xs, e := m.service.ListReviews(m.ctx)
		items := make([]reviewItem, len(xs))
		for i, x := range xs {
			items[i] = reviewItem{x.ID, x.Title, x.Status, x.Type, x.ReviewState}
		}
		return loaded{items, e}
	}
}
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch x := msg.(type) {
	case loaded:
		m.items, m.err = x.items, x.err
	case tea.KeyPressMsg:
		switch x.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, m.Init()
		case "down", "j":
			if m.selected < len(m.items)-1 {
				m.selected++
			}
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		}
	}
	return m, nil
}
func (m model) View() tea.View {
	if m.err != nil {
		return tea.NewView("Review list unavailable\nPress q to quit.\n")
	}
	var b strings.Builder
	b.WriteString("Human reviews\n\n")
	if len(m.items) == 0 {
		b.WriteString("No open reviews.\n")
	}
	for i, x := range m.items {
		p := " "
		if i == m.selected {
			p = ">"
		}
		fmt.Fprintf(&b, "%s %s %s %s %s %s\n", p, x.ID, x.Title, x.Status, x.Type, x.ReviewState)
	}
	b.WriteString("\nr refresh • q quit\n")
	return tea.NewView(b.String())
}
func RunLocal(ctx context.Context, noColor bool, root string) error {
	return Runner.Run(model{ctx: ctx, noColor: noColor, service: newService(root)}, tea.WithoutSignals())
}
