package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/Hitesh-K-Murali/terminal-code/internal/engine"
	"github.com/Hitesh-K-Murali/terminal-code/internal/provider"
)

// State tracks what the UI is currently doing.
type uiState int

const (
	stateIdle      uiState = iota // Waiting for user input
	stateThinking                 // Sent request, waiting for first chunk
	stateStreaming                // Receiving chunks
	stateToolCall                 // Tool is executing
)

// Message types for Bubbletea's update loop.
type (
	streamChunkMsg struct{ text string }
	streamDoneMsg  struct{ usage *provider.Usage }
	streamErrorMsg struct{ err error }
	tickMsg        struct{}
)

type chatMessage struct {
	role      string // "user", "assistant", "tool", "error"
	content   string
	timestamp time.Time
}

// Model is the root Bubbletea model.
type Model struct {
	engine    *engine.Engine
	modelName string
	toolCount int
	commands  map[string]SlashCommand

	// Components
	input    textarea.Model
	viewport viewport.Model
	spinner  spinner.Model

	// State
	state     uiState
	messages  []chatMessage
	streamBuf string
	streamCh  <-chan provider.StreamEvent
	cancelFn  context.CancelFunc

	// Layout
	width  int
	height int
	ready  bool

	// Stats
	totalInputTokens  int
	totalOutputTokens int
	totalCost         float64

	// Cached renderer (created once, not per-frame)
	renderer *glamour.TermRenderer
}

func NewModel(eng *engine.Engine, model string) *Model {
	// Input: prompt-style textarea
	ta := textarea.New()
	ta.Placeholder = "Ask anything..."
	ta.Prompt = "› "
	ta.Focus()
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.SetHeight(1) // Start with 1 line, grows as needed
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetKeys("ctrl+d")
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle() // No highlight on current line
	ta.FocusedStyle.Prompt = stylePrompt
	ta.BlurredStyle.Prompt = styleMuted

	// Spinner for thinking/tool states
	sp := spinner.New()
	sp.Spinner = spinner.Points
	sp.Style = styleSpinner

	return &Model{
		engine:    eng,
		modelName: model,
		commands:  RegisterCommands(),
		input:     ta,
		spinner:   sp,
		state:     stateIdle,
	}
}

// SetToolCount sets the displayed tool count in the header.
func (m *Model) SetToolCount(n int) {
	m.toolCount = n
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.state != stateIdle {
				// Cancel active operation
				if m.cancelFn != nil {
					m.cancelFn()
				}
				m.state = stateIdle
				m.streamBuf = ""
				m.streamCh = nil
				m.messages = append(m.messages, chatMessage{
					role:      "error",
					content:   "Cancelled",
					timestamp: time.Now(),
				})
				m.refreshViewport()
				break
			}
			return m, tea.Quit

		case "enter":
			if m.state != stateIdle {
				break
			}
			input := strings.TrimSpace(m.input.Value())
			if input == "" {
				break
			}
			if input == "/quit" || input == "/exit" {
				return m, tea.Quit
			}

			// Slash commands
			if strings.HasPrefix(input, "/") {
				result := m.handleCommand(input)
				if result != "" {
					m.messages = append(m.messages, chatMessage{
						role: "user", content: input, timestamp: time.Now()})
					m.messages = append(m.messages, chatMessage{
						role: "assistant", content: result, timestamp: time.Now()})
				}
				m.input.Reset()
				m.refreshViewport()
				break
			}

			// Send to LLM
			m.messages = append(m.messages, chatMessage{
				role: "user", content: input, timestamp: time.Now()})
			m.input.Reset()
			m.input.SetHeight(1)
			m.state = stateThinking
			m.streamBuf = ""
			m.refreshViewport()

			cmds = append(cmds, m.startStreamCmd(input))
		}

	case streamChunkMsg:
		if m.state == stateThinking {
			m.state = stateStreaming
		}
		m.streamBuf += msg.text
		m.refreshViewport()
		cmds = append(cmds, m.waitForEventCmd())

	case streamDoneMsg:
		if m.streamBuf != "" {
			m.messages = append(m.messages, chatMessage{
				role: "assistant", content: m.streamBuf, timestamp: time.Now()})
		}
		if msg.usage != nil {
			m.totalInputTokens += msg.usage.InputTokens
			m.totalOutputTokens += msg.usage.OutputTokens
			m.totalCost += estimateCost(m.modelName, msg.usage.InputTokens, msg.usage.OutputTokens)
		}
		m.streamBuf = ""
		m.state = stateIdle
		m.streamCh = nil
		m.refreshViewport()

	case streamErrorMsg:
		m.messages = append(m.messages, chatMessage{
			role: "error", content: msg.err.Error(), timestamp: time.Now()})
		m.streamBuf = ""
		m.state = stateIdle
		m.streamCh = nil
		m.refreshViewport()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		if m.state == stateThinking || m.state == stateToolCall {
			m.refreshViewport() // Redraw spinner
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()
	}

	// Update input component (only when idle)
	if m.state == stateIdle {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		cmds = append(cmds, cmd)

		// Auto-resize input: grow/shrink based on content (1-5 lines)
		lines := strings.Count(m.input.Value(), "\n") + 1
		if lines > 5 {
			lines = 5
		}
		if lines < 1 {
			lines = 1
		}
		if m.input.Height() != lines {
			m.input.SetHeight(lines)
			m.recalcLayout()
		}
	}

	// Update viewport
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) View() string {
	if !m.ready {
		return "  Initializing..."
	}

	header := m.renderHeader()
	chat := m.viewport.View()
	hints := m.renderHints()
	input := styleInputArea.Width(m.width).Render(m.input.View())

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		chat,
		hints,
		input,
	)
}

// --- Header ---

func (m *Model) renderHeader() string {
	left := styleHeaderBrand.Render(" tc") + "  " + styleHeaderModel.Render(m.modelName)

	cost := fmt.Sprintf("$%.4f", m.totalCost)
	tools := fmt.Sprintf("%d tools", m.toolCount)
	right := styleHeaderMeta.Render(cost + "  " + tools)

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	bar := left + strings.Repeat(" ", gap) + right
	return styleHeaderBar.Width(m.width).Render(bar)
}

// --- Hints ---

func (m *Model) renderHints() string {
	var parts []string

	switch m.state {
	case stateIdle:
		parts = []string{
			styleHintKey.Render("⏎") + " " + styleHintDesc.Render("send"),
			styleHintKey.Render("^D") + " " + styleHintDesc.Render("newline"),
			styleHintKey.Render("^C") + " " + styleHintDesc.Render("quit"),
			styleHintDesc.Render("/help"),
		}
	case stateThinking, stateStreaming, stateToolCall:
		parts = []string{
			styleHintKey.Render("^C") + " " + styleHintDesc.Render("cancel"),
		}
	}

	return styleHintBar.Width(m.width).Render(strings.Join(parts, "   "))
}

// --- Messages ---

func (m *Model) refreshViewport() {
	if m.renderer == nil {
		contentWidth := m.width - 6
		if contentWidth < 40 {
			contentWidth = 40
		}
		m.renderer, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
			glamour.WithWordWrap(contentWidth),
		)
	}

	var sb strings.Builder

	for _, msg := range m.messages {
		ts := msg.timestamp.Format("15:04")

		switch msg.role {
		case "user":
			label := styleUserDot.Render("●") + " " + styleUserLabel.Render("You")
			tsStr := styleTimestamp.Render(ts)
			gap := m.width - lipgloss.Width(label) - lipgloss.Width(tsStr) - 4
			if gap < 1 {
				gap = 1
			}
			sb.WriteString("\n" + label + strings.Repeat(" ", gap) + tsStr + "\n")
			sb.WriteString(styleUserMsg.Render(msg.content) + "\n")

		case "assistant":
			label := styleAssistDot.Render("◆") + " " + styleAssistLabel.Render("tc")
			tsStr := styleTimestamp.Render(ts)
			gap := m.width - lipgloss.Width(label) - lipgloss.Width(tsStr) - 4
			if gap < 1 {
				gap = 1
			}
			sb.WriteString("\n" + label + strings.Repeat(" ", gap) + tsStr + "\n")
			rendered := renderMarkdown(m.renderer, msg.content)
			sb.WriteString(styleAssistMsg.Render(rendered) + "\n")

		case "tool":
			sb.WriteString(styleToolLine.Render("  ┊ ") + styleToolCall.Render(msg.content) + "\n")

		case "error":
			sb.WriteString("\n" + styleError.Render("✗ Error: "+msg.content) + "\n")
		}
	}

	// Streaming state
	switch m.state {
	case stateThinking:
		label := styleAssistDot.Render("◆") + " " + styleAssistLabel.Render("tc")
		sb.WriteString("\n" + label + "  " + m.spinner.View() + " " +
			styleStreamStatus.Render("thinking") + "\n")

	case stateStreaming:
		if m.streamBuf != "" {
			label := styleAssistDot.Render("◆") + " " + styleAssistLabel.Render("tc")
			sb.WriteString("\n" + label + "\n")
			rendered := renderMarkdown(m.renderer, m.streamBuf)
			sb.WriteString(styleAssistMsg.Render(rendered))
			sb.WriteString(styleStreamCursor.Render("▌") + "\n")
		}

	case stateToolCall:
		label := styleAssistDot.Render("◆") + " " + styleAssistLabel.Render("tc")
		sb.WriteString("\n" + label + "  " + m.spinner.View() + "\n")
	}

	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

func renderMarkdown(renderer *glamour.TermRenderer, content string) string {
	if renderer == nil {
		return content
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		return content
	}
	return strings.TrimRight(rendered, "\n")
}

// --- Layout ---

func (m *Model) recalcLayout() {
	headerHeight := 2 // 1 line + border
	hintHeight := 2   // 1 line + border
	inputHeight := m.input.Height() + 1

	chatHeight := m.height - headerHeight - hintHeight - inputHeight
	if chatHeight < 3 {
		chatHeight = 3
	}

	if !m.ready {
		m.viewport = viewport.New(m.width, chatHeight)
		m.ready = true
	} else {
		m.viewport.Width = m.width
		m.viewport.Height = chatHeight
	}
	m.input.SetWidth(m.width - 2)

	// Invalidate cached renderer on width change
	m.renderer = nil

	m.refreshViewport()
}

// --- Streaming ---

func (m *Model) startStreamCmd(input string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelFn = cancel

		ch, err := m.engine.Send(ctx, input)
		if err != nil {
			cancel()
			return streamErrorMsg{err: err}
		}

		m.streamCh = ch
		return readOneEvent(ch)
	}
}

func (m *Model) waitForEventCmd() tea.Cmd {
	ch := m.streamCh
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		return readOneEvent(ch)
	}
}

func readOneEvent(ch <-chan provider.StreamEvent) tea.Msg {
	event, ok := <-ch
	if !ok {
		return streamDoneMsg{}
	}
	switch event.Type {
	case provider.EventText:
		return streamChunkMsg{text: event.Text}
	case provider.EventDone:
		return streamDoneMsg{usage: event.Usage}
	case provider.EventError:
		return streamErrorMsg{err: event.Error}
	default:
		return streamDoneMsg{}
	}
}

// --- Commands ---

func (m *Model) handleCommand(input string) string {
	parts := strings.SplitN(input, " ", 2)
	cmdName := parts[0]
	args := ""
	if len(parts) > 1 {
		args = parts[1]
	}

	cmd, ok := m.commands[cmdName]
	if !ok {
		return fmt.Sprintf("Unknown command: `%s`. Type `/help` for available commands.", cmdName)
	}

	if cmd.Handler == nil {
		return ""
	}

	return cmd.Handler(m, args)
}

// --- Cost estimation ---

func estimateCost(model string, input, output int) float64 {
	type pricing struct{ in, out float64 }
	prices := map[string]pricing{
		"claude-opus-4-20250514":   {15.0, 75.0},
		"claude-sonnet-4-20250514": {3.0, 15.0},
		"claude-haiku-4-20250414":  {0.25, 1.25},
		"gpt-4o":                  {2.50, 10.0},
		"gpt-4o-mini":             {0.15, 0.60},
		"o1":                      {15.0, 60.0},
		"o3-mini":                 {1.10, 4.40},
	}

	p, ok := prices[model]
	if !ok {
		p = pricing{3.0, 15.0} // default estimate
	}

	return float64(input)*p.in/1_000_000 + float64(output)*p.out/1_000_000
}
