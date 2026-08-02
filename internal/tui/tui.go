// Package tui contains SMT's small, terminal-only human review surface.
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/parmcoder/smt/internal/beads"
	"github.com/parmcoder/smt/internal/prereq"
	"github.com/parmcoder/smt/internal/scaffold"
)

// AppRunner is the terminal seam used by the CLI and tests.
type AppRunner interface {
	Run(tea.Model, ...tea.ProgramOption) error
}

type bubbleApp struct{}

func (bubbleApp) Run(model tea.Model, options ...tea.ProgramOption) error {
	_, err := tea.NewProgram(model, options...).Run()
	return err
}

// Run opens the full-screen terminal application. It performs no domain I/O.
func Run(ctx context.Context, noColor bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m := NewModel(noColor)
	m.context = ctx
	return bubbleApp{}.Run(m)
}

// RunLocal creates the production adapter at the workspace root.
func RunLocal(ctx context.Context, noColor bool, root string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m := NewModelWithOperations(noColor, NewLocalOperations(root))
	m.context = ctx
	return bubbleApp{}.Run(m)
}

// RunSetup opens the setup tab; destination is retained as a visible notice
// until the setup form is completed by a human.
func RunSetup(ctx context.Context, noColor bool, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m := NewModel(noColor)
	m.context = ctx
	m.destination.SetValue(destination)
	m.notice = "Setup destination: " + destination
	return bubbleApp{}.Run(m)
}

// RunSetupLocal opens Setup with the same production adapter as the full UI.
func RunSetupLocal(ctx context.Context, noColor bool, root, destination string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m := NewModelWithOperations(noColor, NewLocalOperations(root))
	m.context = ctx
	m.destination.SetValue(destination)
	m.notice = "Setup destination: " + destination
	return bubbleApp{}.Run(m)
}

type Model struct {
	tab, width, height int
	noColor            bool
	busy               bool
	cancel             context.CancelFunc
	notice             string
	ops                Operations
	context            context.Context
	opID               uint64
	destination        textinput.Model
	setupFocus         int
	selection          scaffold.Selection
	setupReady         bool
	setupResult        string
	setupInitialized   bool
	findings           []prereq.Finding
	setupVersion       uint64
	workItems          []workItem
	workSelection      int
	workState          workLoadState
	workHandoff        textinput.Model
	workEvidence       textinput.Model
	workFocus          workFocus
	queueOutcome       queueOutcome
	reviewItems        []reviewItem
	reviewSelection    int
	reviewLoad         reviewLoadState
	passForm           bool
	passReviewer       textinput.Model
	passEvidence       textinput.Model
	passResult         string
	passFocus          passFocus
}

// workItem is the private, presentation-only projection of a Beads issue.
// It deliberately excludes labels, metadata, and dependency data.
type workItem struct {
	ID, Title, Status, Type, ReviewState string
}

type queueOutcome struct {
	FeatureID, ReviewID, Recovery string
}

type reviewItem struct {
	ID, Title, Status, Type, ReviewState string
}

type reviewLoadState string
type passFocus int

const (
	passFocusReviewer passFocus = iota
	passFocusEvidence
	passFocusSubmit
)

const (
	reviewLoadLoading     reviewLoadState = "loading"
	reviewLoadLoaded      reviewLoadState = "loaded"
	reviewLoadEmpty       reviewLoadState = "empty"
	reviewLoadUnavailable reviewLoadState = "unavailable"
)

type workLoadState string

const (
	workLoadLoading     workLoadState = "loading"
	workLoadLoaded      workLoadState = "loaded"
	workLoadEmpty       workLoadState = "empty"
	workLoadUnavailable workLoadState = "unavailable"
)

type workFocus int

const (
	workFocusList workFocus = iota
	workFocusHandoff
	workFocusEvidence
	workFocusQueue
)

// Operations is the thin async boundary between rendering and accepted SMT
// services. It deliberately contains no business rules.
type Operations interface {
	Check(context.Context, prereq.Requirements) (prereq.Result, error)
	Init(context.Context, string, scaffold.Selection) (scaffold.Result, error)
	ReadyWork(context.Context) ([]beads.Issue, error)
	QueueReview(context.Context, string, string, string) (beads.QueueResult, error)
	ListReviews(context.Context) ([]beads.Issue, error)
	HumanPass(context.Context, string, string, string) (beads.Recovery, error)
	HumanFail(context.Context, string, beads.FailureInput) (beads.Recovery, error)
	RequeueAfterFix(context.Context, string) (beads.Recovery, error)
	ReleaseReadiness(context.Context) (beads.ReleaseReadiness, error)
}

// LocalOperations composes the existing SMT services for the terminal UI.
type LocalOperations struct {
	prereq   prereq.Inspector
	scaffold *scaffold.Service
	beads    *beads.Service
}

func NewLocalOperations(root string) *LocalOperations {
	p := prereq.New()
	return &LocalOperations{prereq: p, scaffold: scaffold.New(p), beads: beads.New(root, beads.CommandRunner{})}
}
func (o *LocalOperations) Check(ctx context.Context, r prereq.Requirements) (prereq.Result, error) {
	return o.prereq.Check(ctx, r)
}
func (o *LocalOperations) Init(ctx context.Context, d string, s scaffold.Selection) (scaffold.Result, error) {
	return o.scaffold.Init(ctx, d, s)
}
func (o *LocalOperations) ReadyWork(ctx context.Context) ([]beads.Issue, error) {
	return o.beads.ReadyWork(ctx)
}
func (o *LocalOperations) QueueReview(ctx context.Context, a, b, c string) (beads.QueueResult, error) {
	return o.beads.QueueReview(ctx, a, b, c)
}
func (o *LocalOperations) ListReviews(ctx context.Context) ([]beads.Issue, error) {
	return o.beads.ListReviews(ctx)
}
func (o *LocalOperations) HumanPass(ctx context.Context, a, b, c string) (beads.Recovery, error) {
	return o.beads.HumanPass(ctx, a, b, c)
}
func (o *LocalOperations) HumanFail(ctx context.Context, a string, b beads.FailureInput) (beads.Recovery, error) {
	return o.beads.HumanFail(ctx, a, b)
}
func (o *LocalOperations) RequeueAfterFix(ctx context.Context, a string) (beads.Recovery, error) {
	return o.beads.RequeueAfterFix(ctx, a)
}
func (o *LocalOperations) ReleaseReadiness(ctx context.Context) (beads.ReleaseReadiness, error) {
	return o.beads.ReleaseReadiness(ctx)
}

type resultMsg struct {
	value        any
	err          error
	id           uint64
	setupVersion uint64
	kind         operationKind
}
type operationKind string

const (
	opNone        operationKind = ""
	opCheck       operationKind = "check"
	opInit        operationKind = "init"
	opReadyWork   operationKind = "ready-work"
	opQueueReview operationKind = "queue-review"
	opListReviews operationKind = "list-reviews"
)

// Command runs all I/O behind a typed Bubble Tea message and retains a cancel
// function so esc/ctrl-c is safe while the command is busy.
func (m *Model) Command(run func(context.Context) (any, error)) tea.Cmd {
	return m.commandWith(opNone, m.setupVersion, run)
}
func (m *Model) commandWith(kind operationKind, version uint64, run func(context.Context) (any, error)) tea.Cmd {
	ctx, cancel := context.WithCancel(m.context)
	m.busy = true
	m.cancel = cancel
	m.opID++
	id := m.opID
	return func() tea.Msg { value, err := run(ctx); return resultMsg{value, err, id, version, kind} }
}
func (m *Model) startCheck() tea.Cmd {
	requirements := scaffold.Requirements(m.selection)
	m.setupReady = false
	m.findings = nil
	m.setupResult = ""
	m.notice = "Checking prerequisites"
	return m.commandWith(opCheck, m.setupVersion, func(ctx context.Context) (any, error) { return m.ops.Check(ctx, requirements) })
}
func (m *Model) startInit() tea.Cmd {
	return m.commandWith(opInit, m.setupVersion, func(ctx context.Context) (any, error) { return m.ops.Init(ctx, m.destination.Value(), m.selection) })
}
func (m *Model) startReadyWork() tea.Cmd {
	m.workItems = nil
	m.workSelection = 0
	m.workState = workLoadLoading
	return m.commandWith(opReadyWork, m.setupVersion, func(ctx context.Context) (any, error) { return m.ops.ReadyWork(ctx) })
}
func (m *Model) startQueueReview(featureID, handoff, evidence string) tea.Cmd {
	m.queueOutcome = queueOutcome{}
	m.notice = "Queueing human E2E review"
	return m.commandWith(opQueueReview, m.setupVersion, func(ctx context.Context) (any, error) {
		return m.ops.QueueReview(ctx, featureID, handoff, evidence)
	})
}
func (m *Model) startListReviews() tea.Cmd {
	m.reviewItems = nil
	m.reviewSelection = 0
	m.reviewLoad = reviewLoadLoading
	m.notice = "Loading human E2E reviews"
	return m.commandWith(opListReviews, m.setupVersion, func(ctx context.Context) (any, error) {
		return m.ops.ListReviews(ctx)
	})
}

var tabs = []string{"Setup", "Work", "Human Review", "Workspace Health"}

func NewModel(noColor bool) Model {
	input := textinput.New()
	input.SetValue(".")
	input.Focus()
	handoff := textinput.New()
	handoff.Prompt = ""
	handoff.Placeholder = "docs/handoff.md"
	evidence := textinput.New()
	evidence.Prompt = ""
	evidence.Placeholder = "evidence/result.md"
	reviewer := textinput.New()
	reviewer.Prompt = ""
	reviewer.Placeholder = "reviewer"
	passEvidence := textinput.New()
	passEvidence.Prompt = ""
	passEvidence.Placeholder = "review evidence"
	return Model{noColor: noColor, context: context.Background(), destination: input, selection: scaffold.Selection{Web: true, API: true, Database: true, DevOps: true, Codex: true}, workHandoff: handoff, workEvidence: evidence, passReviewer: reviewer, passEvidence: passEvidence}
}
func NewModelWithOperations(noColor bool, ops Operations) Model {
	m := NewModel(noColor)
	m.ops = ops
	return m
}
func (m Model) Init() tea.Cmd { return nil }
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case resultMsg:
		if v.id != m.opID {
			return m, nil
		}
		m.busy = false
		m.cancel = nil
		if v.kind == opListReviews {
			m.reviewItems = nil
			m.reviewSelection = 0
			if v.err != nil {
				m.reviewLoad = reviewLoadUnavailable
				m.notice = "Human reviews are unavailable; verify Beads and retry"
				return m, nil
			}
			issues, ok := v.value.([]beads.Issue)
			if !ok {
				m.reviewLoad = reviewLoadUnavailable
				m.notice = "Human reviews are unavailable; verify Beads and retry"
				return m, nil
			}
			for _, issue := range issues {
				m.reviewItems = append(m.reviewItems, reviewItem{
					ID:          issue.ID,
					Title:       issue.Title,
					Status:      issue.Status,
					Type:        issue.Type,
					ReviewState: issue.ReviewState,
				})
			}
			if len(m.reviewItems) == 0 {
				m.reviewLoad = reviewLoadEmpty
				m.notice = "No human E2E reviews available"
				return m, nil
			}
			m.reviewLoad = reviewLoadLoaded
			m.notice = "Human E2E reviews loaded"
			return m, nil
		}
		if v.kind == opQueueReview {
			result, ok := v.value.(beads.QueueResult)
			if v.err != nil {
				m.queueOutcome = queueOutcome{}
				if ok {
					m.queueOutcome = safeQueueOutcome(result)
				}
				m.notice = "Review queueing failed safely"
				return m, nil
			}
			if !ok {
				m.queueOutcome = queueOutcome{}
				m.notice = "Review queueing result was invalid"
				return m, nil
			}
			m.queueOutcome = safeQueueOutcome(result)
			m.notice = "Human E2E review queued"
			return m, nil
		}
		if v.kind == opReadyWork {
			m.workItems = nil
			m.workSelection = 0
			if v.err != nil {
				m.workState = workLoadUnavailable
				m.notice = "Ready work is unavailable; verify Beads and retry"
				return m, nil
			}
			issues, ok := v.value.([]beads.Issue)
			if !ok {
				m.workState = workLoadUnavailable
				m.notice = "Ready work is unavailable; verify Beads and retry"
				return m, nil
			}
			for _, issue := range issues {
				m.workItems = append(m.workItems, workItem{
					ID:          issue.ID,
					Title:       issue.Title,
					Status:      issue.Status,
					Type:        issue.Type,
					ReviewState: issue.ReviewState,
				})
			}
			if len(m.workItems) == 0 {
				m.workState = workLoadEmpty
				m.notice = "No ready work available"
				return m, nil
			}
			m.workState = workLoadLoaded
			m.notice = "Ready work loaded"
			return m, nil
		}
		if v.kind == opInit && v.setupVersion != m.setupVersion {
			return m, nil
		}
		if v.kind == opCheck && v.setupVersion != m.setupVersion {
			m.setupReady = false
			m.findings = nil
			m.notice = "Setup changed; re-check prerequisites"
			return m, nil
		}
		if v.kind == opCheck {
			if v.err != nil {
				m.setupReady = false
				m.findings = nil
				m.notice = "Operation failed safely"
				return m, nil
			}
			result, ok := v.value.(prereq.Result)
			if !ok {
				m.setupReady = false
				m.findings = nil
				m.notice = "Operation result was invalid"
				return m, nil
			}
			m.findings = result.Findings
			m.setupReady = result.Ready()
			m.notice = "Operation complete"
		} else if v.kind == opInit {
			if v.err != nil {
				m.setupReady = false
				m.setupResult = "Recovery: re-check prerequisites before retrying initialization"
				m.notice = "Workspace initialization failed safely"
				return m, nil
			}
			result, ok := v.value.(scaffold.Result)
			if !ok {
				m.setupReady = false
				m.setupResult = "Recovery: re-check prerequisites before retrying initialization"
				m.notice = "Workspace initialization result was invalid"
				return m, nil
			}
			m.setupResult = "Initialized " + result.Destination
			m.setupInitialized = true
			if len(result.Repositories) > 0 {
				m.setupResult += " (" + strings.Join(result.Repositories, ", ") + ")"
			}
			m.notice = "Workspace initialized"
		} else if v.err != nil {
			m.notice = "Operation failed safely"
		} else {
			m.notice = "Operation complete"
		}
	case tea.WindowSizeMsg:
		m.width, m.height = v.Width, v.Height
	case tea.KeyPressMsg:
		key := v.String()
		if key == "q" {
			if m.cancel != nil {
				m.cancel()
			}
			return m, tea.Quit
		}
		if (key == "ctrl+c" || key == "esc") && m.busy && m.cancel != nil {
			m.cancel()
			m.cancel = nil
			m.busy = false
			m.opID++
			m.notice = "Operation cancelled"
			return m, nil
		}
		if m.busy {
			return m, nil
		}
		if m.tab == 2 && m.passForm && key == "esc" {
			m.closePassForm()
			return m, nil
		}
		if m.tab == 2 && m.passForm && key == "tab" {
			return m, m.setPassFocus((m.passFocus + 1) % 3)
		}
		if m.tab == 2 && m.passForm && key == "shift+tab" {
			return m, m.setPassFocus((m.passFocus + 2) % 3)
		}
		if m.tab == 2 && m.passForm && m.passFocus == passFocusReviewer {
			var cmd tea.Cmd
			m.passReviewer, cmd = m.passReviewer.Update(v)
			return m, cmd
		}
		if m.tab == 2 && m.passForm && m.passFocus == passFocusEvidence {
			var cmd tea.Cmd
			m.passEvidence, cmd = m.passEvidence.Update(v)
			return m, cmd
		}
		if m.tab == 1 && m.workFocus == workFocusQueue && key == "enter" {
			featureID, handoff, evidence, ok := m.validateQueueInput()
			if !ok {
				return m, nil
			}
			return m, m.startQueueReview(featureID, handoff, evidence)
		}
		if m.tab == 2 {
			if key == "p" {
				m.openPassForm()
				return m, nil
			}
			if key == "r" && m.ops != nil {
				return m, m.startListReviews()
			}
			if m.reviewLoad == reviewLoadLoaded {
				switch key {
				case "up", "j":
					if m.reviewSelection > 0 {
						m.reviewSelection--
					}
					return m, nil
				case "down", "k":
					if m.reviewSelection < len(m.reviewItems)-1 {
						m.reviewSelection++
					}
					return m, nil
				}
			}
		}
		if m.tab == 1 && m.workState == workLoadLoaded {
			if key == "tab" {
				return m, m.setWorkFocus((m.workFocus + 1) % 4)
			}
			if key == "shift+tab" {
				return m, m.setWorkFocus((m.workFocus + 3) % 4)
			}
			switch m.workFocus {
			case workFocusList:
				switch key {
				case "r":
					if m.ops != nil {
						return m, m.startReadyWork()
					}
				case "up", "j":
					if m.workSelection > 0 {
						m.workSelection--
					}
					return m, nil
				case "down", "k":
					if m.workSelection < len(m.workItems)-1 {
						m.workSelection++
					}
					return m, nil
				}
			case workFocusHandoff:
				var cmd tea.Cmd
				m.workHandoff, cmd = m.workHandoff.Update(v)
				return m, cmd
			case workFocusEvidence:
				var cmd tea.Cmd
				m.workEvidence, cmd = m.workEvidence.Update(v)
				return m, cmd
			}
		}
		if m.tab == 0 {
			before := m.destination.Value()
			if key == "tab" || key == "down" {
				return m, m.setSetupFocus((m.setupFocus + 1) % 7)
			}
			if key == "shift+tab" || key == "up" {
				return m, m.setSetupFocus((m.setupFocus + 6) % 7)
			}
			if m.setupFocus == 0 && key != "right" && key != "left" && key != "h" && key != "l" && key != "esc" && key != "ctrl+c" && key != "q" {
				var cmd tea.Cmd
				m.destination, cmd = m.destination.Update(v)
				if before != m.destination.Value() {
					m.invalidateSetup()
				}
				return m, cmd
			}
			if key == " " || key == "enter" {
				if m.setupFocus == 5 {
					if !m.selection.Web && !m.selection.API && !m.selection.Database && !m.selection.DevOps {
						m.notice = "Select at least one component"
						return m, nil
					}
					if m.ops != nil {
						return m, m.startCheck()
					}
				}
				if m.setupFocus == 6 {
					if m.setupInitialized {
						m.notice = "Workspace is already initialized"
						return m, nil
					}
					if !m.setupReady {
						m.notice = "Prerequisites are required"
						return m, nil
					}
					if m.ops != nil {
						return m, m.startInit()
					}
				}
				switch m.setupFocus {
				case 1:
					m.selection.Web = !m.selection.Web
				case 2:
					m.selection.API = !m.selection.API
				case 3:
					m.selection.Database = !m.selection.Database
				case 4:
					m.selection.DevOps = !m.selection.DevOps
				}
				m.invalidateSetup()
				return m, nil
			}
		}
		if key == "right" || key == "l" || key == "tab" {
			m.tab = (m.tab + 1) % len(tabs)
		}
		if key == "left" || key == "h" || key == "shift+tab" {
			m.tab = (m.tab + len(tabs) - 1) % len(tabs)
		}
		if key == "enter" && m.ops != nil && !m.busy {
			return m, m.tabCommand()
		}
	}
	return m, nil
}

func (m *Model) setSetupFocus(index int) tea.Cmd {
	m.setupFocus = index
	if index == 0 {
		return m.destination.Focus()
	} else {
		m.destination.Blur()
		return nil
	}
}
func (m *Model) setWorkFocus(focus workFocus) tea.Cmd {
	m.workFocus = focus
	m.workHandoff.Blur()
	m.workEvidence.Blur()
	switch focus {
	case workFocusHandoff:
		return m.workHandoff.Focus()
	case workFocusEvidence:
		return m.workEvidence.Focus()
	default:
		return nil
	}
}

func (m *Model) openPassForm() {
	if m.reviewLoad != reviewLoadLoaded || m.reviewSelection < 0 || m.reviewSelection >= len(m.reviewItems) || strings.TrimSpace(m.reviewItems[m.reviewSelection].ID) == "" {
		m.notice = "Select a queued human review before recording a pass"
		return
	}
	state := m.reviewItems[m.reviewSelection].ReviewState
	if state != "queued" && state != "retest-queued" {
		m.notice = "Only queued human reviews can be passed"
		return
	}
	m.passForm = true
	m.setPassFocus(passFocusReviewer)
	m.passResult = ""
	m.passFocus = passFocusReviewer
	m.notice = "Record human pass"
}
func (m *Model) setPassFocus(f passFocus) tea.Cmd {
	m.passFocus = f
	m.passReviewer.Blur()
	m.passEvidence.Blur()
	if f == passFocusReviewer {
		return m.passReviewer.Focus()
	}
	if f == passFocusEvidence {
		return m.passEvidence.Focus()
	}
	return nil
}
func (m *Model) closePassForm() {
	m.passForm = false
	m.passReviewer.Reset()
	m.passEvidence.Reset()
	m.passResult = ""
	m.notice = "Human pass form closed"
}

func (m *Model) validateQueueInput() (string, string, string, bool) {
	if m.workState != workLoadLoaded || m.workSelection < 0 || m.workSelection >= len(m.workItems) || strings.TrimSpace(m.workItems[m.workSelection].ID) == "" {
		m.notice = "Select ready work before queueing review"
		return "", "", "", false
	}
	featureID := m.workItems[m.workSelection].ID
	handoff, guidance := workspaceRelativePath("Handoff", m.workHandoff.Value())
	if guidance != "" {
		m.notice = guidance
		return "", "", "", false
	}
	evidence, guidance := workspaceRelativePath("Evidence", m.workEvidence.Value())
	if guidance != "" {
		m.notice = guidance
		return "", "", "", false
	}
	m.workHandoff.SetValue(handoff)
	m.workEvidence.SetValue(evidence)
	return featureID, handoff, evidence, true
}

func workspaceRelativePath(field, value string) (string, string) {
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", field + " path contains control characters"
		}
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", field + " path is required"
	}
	clean := filepath.Clean(trimmed)
	if filepath.IsAbs(trimmed) || clean == "." {
		if clean == "." {
			return "", field + " path must name a workspace-relative file"
		}
		return "", field + " path must be a workspace-relative file"
	}
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", field + " path must be a workspace-relative file"
	}
	return clean, ""
}
func (m *Model) invalidateSetup() {
	m.setupReady = false
	m.setupResult = ""
	m.setupInitialized = false
	m.findings = nil
	m.setupVersion++
	m.notice = "Setup changed; re-check prerequisites"
}

func (m *Model) tabCommand() tea.Cmd {
	switch m.tab {
	case 0:
		return m.startCheck()
	case 1:
		return m.startReadyWork()
	case 2:
		return m.startListReviews()
	default:
		return m.Command(func(ctx context.Context) (any, error) { return m.ops.ReleaseReadiness(ctx) })
	}
}
func (m Model) View() tea.View {
	var view tea.View
	view.AltScreen = true
	if m.width > 0 && m.width < 40 {
		view.SetContent("SMT\nTerminal too small: too narrow (need 40 columns).\nq quit")
		return view
	}
	if m.height > 0 && m.height < 10 {
		view.SetContent("SMT\nTerminal too small: too short (need 10 rows).\nq quit")
		return view
	}
	var nav []string
	for i, t := range tabs {
		marker := " "
		if i == m.tab {
			marker = ">"
		}
		nav = append(nav, marker+" "+t)
	}
	content := []string{"Setup: check prerequisites; installation is always human-directed.", "Work: select ready engineering work and queue a human E2E review.", "Human Review: pass or fail only after a human decision.", "Workspace Health: inspect configuration, status, prerequisites, and release blockers."}[m.tab]
	if m.tab == 0 {
		mark := func(i int) string {
			if m.setupFocus == i {
				return ">"
			}
			return " "
		}
		box := func(v bool) string {
			if v {
				return "[x]"
			}
			return "[ ]"
		}
		action := "Check prerequisites"
		if len(m.findings) > 0 {
			action = "Re-check prerequisites"
		}
		content = fmt.Sprintf("%s Destination: %s\n%s %s Web\n%s %s API\n%s %s Database\n%s %s DevOps\n  [x] Codex required\n%s [ %s ]\n%s [ Initialize workspace ]", mark(0), m.destination.Value(), mark(1), box(m.selection.Web), mark(2), box(m.selection.API), mark(3), box(m.selection.Database), mark(4), box(m.selection.DevOps), mark(5), action, mark(6))
		if m.setupReady {
			content += "\nPrerequisites are ready"
		} else if len(m.findings) > 0 {
			content += "\nPrerequisites are not ready"
		}
		if m.setupResult != "" {
			content += "\n" + m.setupResult
		}
		for _, finding := range m.findings {
			content += "\n" + finding.ID + " " + string(finding.Status) + ": " + finding.Message
			if finding.Guidance != "" {
				content += "\n  Guidance: " + finding.Guidance
			}
		}
		if m.width > 0 {
			available := m.width - 2
			if available < 20 {
				available = 20
			}
			content = wrapLines(content, available)
		}
	}
	if m.tab == 1 {
		content = m.workContent()
		if m.width > 0 {
			available := m.width - 2
			if available < 20 {
				available = 20
			}
			content = wrapLines(content, available)
		}
	}
	if m.tab == 2 {
		content = m.reviewContent()
		if m.width > 0 {
			available := m.width - 2
			if available < 20 {
				available = 20
			}
			content = wrapLines(content, available)
		}
	}
	navText := strings.Join(nav, "  ")
	if m.width > 0 && m.width < 90 {
		navText = strings.Join(nav, "\n")
	}
	plain := fmt.Sprintf("SMT — Sanovy Mono Tool\n%s\n\n%s\n\n%s\n\n←/h →/l tab navigate • enter select • esc back • q quit", navText, content, m.notice)
	if m.noColor {
		view.SetContent(plain)
	} else {
		title := lipgloss.NewStyle().Bold(true).Underline(true).Render("SMT — Sanovy Mono Tool")
		styledNav := make([]string, len(tabs))
		for i, t := range tabs {
			s := "  " + t
			if i == m.tab {
				s = lipgloss.NewStyle().Bold(true).Render("> " + t)
			}
			styledNav[i] = s
		}
		if m.setupFocus >= 0 {
			content = strings.Replace(content, "> ", lipgloss.NewStyle().Bold(true).Underline(true).Render("> "), 1)
		}
		width := m.width - 2 // two border columns; no horizontal padding exceeds the terminal budget
		if width < 1 {
			width = 1
		}
		card := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 0).Width(width).Render(content)
		status := lipgloss.NewStyle().Italic(true).Render(m.notice)
		help := lipgloss.NewStyle().Faint(true).Render("←/h →/l tab navigate • enter select • esc back • q quit")
		navigation := strings.Join(styledNav, " ")
		if m.width > 0 && m.width < 90 {
			navigation = strings.Join(styledNav, "\n")
		}
		view.SetContent(strings.Join([]string{title, navigation, card, status, help}, "\n\n"))
	}
	return view
}

func wrapLines(text string, width int) string { return ansi.Wrap(text, width, " ") }

func (m Model) workContent() string {
	var content string
	switch m.workState {
	case workLoadLoading:
		content = "Loading ready work..."
	case workLoadEmpty:
		content = "No ready work available. Create or unblock a Beads issue. Press r to refresh."
	case workLoadUnavailable:
		content = "Ready work is unavailable. Verify Beads and retry. Press r to refresh."
	case workLoadLoaded:
		lines := []string{"Ready work (r refresh):"}
		for index, item := range m.workItems {
			marker := " "
			if index == m.workSelection {
				marker = ">"
			}
			lines = append(lines, fmt.Sprintf("%s ID: %s\n  Title: %s\n  Status: %s\n  Type: %s\n  Review state: %s", marker, displayWorkField(item.ID), displayWorkField(item.Title), displayWorkField(item.Status), displayWorkField(item.Type), displayWorkField(item.ReviewState)))
		}
		content = strings.Join(lines, "\n")
	default:
		content = "Press enter to load ready work. Press r to refresh."
	}
	mark := func(focus workFocus) string {
		if m.workFocus == focus {
			return ">"
		}
		return " "
	}
	content += "\n" + mark(workFocusHandoff) + " Handoff: " + m.workInputValue(m.workHandoff) + "\n" + mark(workFocusEvidence) + " Evidence: " + m.workInputValue(m.workEvidence) + "\n" + mark(workFocusQueue) + " [ Queue human E2E review ]"
	if m.queueOutcome.FeatureID != "" {
		content += "\nFeature: " + m.queueOutcome.FeatureID
	}
	if m.queueOutcome.ReviewID != "" {
		content += "\nHuman review: " + m.queueOutcome.ReviewID
	}
	if m.queueOutcome.Recovery != "" {
		content += "\nRecovery: " + m.queueOutcome.Recovery
	}
	return content
}

func (m Model) reviewContent() string {
	switch m.reviewLoad {
	case reviewLoadLoading:
		return "Loading human E2E reviews..."
	case reviewLoadEmpty:
		return "No human E2E reviews available. Press r to refresh."
	case reviewLoadUnavailable:
		return "Human reviews are unavailable. Verify Beads and retry. Press r to refresh."
	case reviewLoadLoaded:
		lines := []string{"Human E2E reviews (r refresh):"}
		for i, item := range m.reviewItems {
			marker := " "
			if i == m.reviewSelection {
				marker = ">"
			}
			lines = append(lines, fmt.Sprintf("%s ID: %s\n  Title: %s\n  Status: %s\n  Type: %s\n  Review state: %s", marker, displayWorkField(item.ID), displayWorkField(item.Title), displayWorkField(item.Status), displayWorkField(item.Type), displayWorkField(item.ReviewState)))
		}
		content := strings.Join(lines, "\n")
		if m.passForm {
			item := m.reviewItems[m.reviewSelection]
			content += "\nPass review: " + displayWorkField(item.ID) + " (" + displayWorkField(item.ReviewState) + ")\nReviewer: " + m.passInputValue(m.passReviewer) + "\nEvidence: " + m.passInputValue(m.passEvidence) + "\n[ Submit Pass ]"
		}
		return content
	default:
		return "Press enter to load human E2E reviews. Press r to refresh."
	}
}
func (m Model) passInputValue(input textinput.Model) string {
	if m.noColor {
		return displayWorkField(input.Value())
	}
	return input.View()
}

func (m Model) workInputValue(input textinput.Model) string {
	if m.noColor {
		return displayWorkField(input.Value())
	}
	return input.View()
}

func displayWorkField(value string) string {
	value = ansi.Strip(strings.ToValidUTF8(value, " "))
	return strings.Map(func(r rune) rune {
		if r <= 0x1f || (r >= 0x7f && r <= 0x9f) {
			return ' '
		}
		return r
	}, value)
}

func safeQueueOutcome(result beads.QueueResult) queueOutcome {
	return queueOutcome{
		FeatureID: strings.TrimSpace(displayWorkField(result.FeatureID)),
		ReviewID:  strings.TrimSpace(displayWorkField(result.ReviewID)),
		Recovery:  strings.TrimSpace(displayWorkField(result.Recovery)),
	}
}
