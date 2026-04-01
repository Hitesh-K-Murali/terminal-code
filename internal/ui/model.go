package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/Hitesh-K-Murali/terminal-code/internal/engine"
	"github.com/Hitesh-K-Murali/terminal-code/internal/provider"
)

// Internal message types for Bubbletea's update loop.
// Each stream event is delivered as a tea.Msg via command chaining:
// startStream → waitForEvent → waitForEvent → ... → streamDoneMsg
type (
	streamChunkMsg struct{ text string }
	streamDoneMsg  struct{ usage *provider.Usage }
	streamErrorMsg struct{ err error }
)

type chatMessage struct {
	role    string
	content string
}

// Model is the root Bubbletea model.
type Model struct {
	engine    *engine.Engine
	modelName string
	commands  map[string]SlashCommand

	textarea  textarea.Model
	viewport  viewport.Model
	messages  []chatMessage
	streaming bool
	streamBuf string
	streamCh  <-chan provider.StreamEvent // Active stream channel
	cancelFn  context.CancelFunc

	width  int
	height int
	ready  bool

	totalInputTokens  int
	totalOutputTokens int
}

func NewModel(eng *engine.Engine, model string) *Model {
	ta := textarea.New()
	ta.Placeholder = "Ask anything... (Enter=send, Ctrl+D=newline, Ctrl+C=quit)"
	ta.Focus()
	ta.CharLimit = 0
	ta.SetWidth(80)
	ta.SetHeight(3)
	ta.ShowLineNumbers = false
	ta.KeyMap.InsertNewline.SetKeys("ctrl+d")

	return &Model{
		engine:    eng,
		modelName: model,
		textarea:  ta,
		commands:  RegisterCommands(),
	}
}

func (m *Model) Init() tea.Cmd {
	return textarea.Blink
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.cancelFn != nil {
				m.cancelFn()
			}
			return m, tea.Quit

		case "enter":
			if m.streaming {
				break
			}
			input := strings.TrimSpace(m.textarea.Value())
			if input == "" {
				break
			}
			if input == "/quit" || input == "/exit" {
				return m, tea.Quit
			}

			// Handle slash commands
			if strings.HasPrefix(input, "/") {
				result := m.handleCommand(input)
				if result != "" {
					m.messages = append(m.messages, chatMessage{role: "user", content: input})
					m.messages = append(m.messages, chatMessage{role: "assistant", content: result})
				}
				m.textarea.Reset()
				m.refreshViewport()
				break
			}

			m.messages = append(m.messages, chatMessage{role: "user", content: input})
			m.textarea.Reset()
			m.streaming = true
			m.streamBuf = ""
			m.refreshViewport()

			cmds = append(cmds, m.startStreamCmd(input))
		}

	case streamChunkMsg:
		m.streamBuf += msg.text
		m.refreshViewport()
		// Chain: wait for next event from the same channel
		cmds = append(cmds, m.waitForEventCmd())

	case streamDoneMsg:
		if m.streamBuf != "" {
			m.messages = append(m.messages, chatMessage{role: "assistant", content: m.streamBuf})
		}
		if msg.usage != nil {
			m.totalInputTokens += msg.usage.InputTokens
			m.totalOutputTokens += msg.usage.OutputTokens
		}
		m.streamBuf = ""
		m.streaming = false
		m.streamCh = nil
		m.refreshViewport()

	case streamErrorMsg:
		m.messages = append(m.messages, chatMessage{role: "error", content: msg.err.Error()})
		m.streamBuf = ""
		m.streaming = false
		m.streamCh = nil
		m.refreshViewport()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		inputHeight := 5
		statusHeight := 1
		headerHeight := 1
		chatHeight := m.height - headerHeight - statusHeight - inputHeight - 2
		if chatHeight < 5 {
			chatHeight = 5
		}

		if !m.ready {
			m.viewport = viewport.New(m.width, chatHeight)
			m.ready = true
		} else {
			m.viewport.Width = m.width
			m.viewport.Height = chatHeight
		}
		m.textarea.SetWidth(m.width - 4)
		m.refreshViewport()
	}

	if !m.streaming {
		var cmd tea.Cmd
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	header := styleHeader.Render(fmt.Sprintf(" tc  %s", styleMuted.Render(m.modelName)))

	status := m.renderStatus()

	input := styleInputBorder.Width(m.width - 4).Render(m.textarea.View())

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		m.viewport.View(),
		status,
		input,
	)
}

func (m *Model) renderStatus() string {
	parts := []string{
		styleMuted.Render(fmt.Sprintf("tokens: %d/%d", m.totalInputTokens, m.totalOutputTokens)),
	}
	if m.streaming {
		parts = append(parts, styleWarning.Render("streaming..."))
	}
	parts = append(parts, styleMuted.Render("/clear /quit"))

	return styleStatusBar.Width(m.width).Render(strings.Join(parts, " | "))
}

func (m *Model) refreshViewport() {
	var sb strings.Builder
	contentWidth := m.width - 8
	if contentWidth < 40 {
		contentWidth = 40
	}

	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(contentWidth),
	)

	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			sb.WriteString("\n" + styleUserLabel.Render(" You") + "\n")
			sb.WriteString(styleUserMsg.Render(msg.content) + "\n")
		case "assistant":
			sb.WriteString("\n" + styleAssistLabel.Render(" tc") + "\n")
			rendered := renderMarkdown(renderer, msg.content)
			sb.WriteString(styleAssistMsg.Render(rendered) + "\n")
		case "error":
			sb.WriteString("\n" + styleError.Render(" Error: "+msg.content) + "\n")
		}
	}

	if m.streaming && m.streamBuf != "" {
		sb.WriteString("\n" + styleAssistLabel.Render(" tc") + "\n")
		rendered := renderMarkdown(renderer, m.streamBuf)
		sb.WriteString(styleAssistMsg.Render(rendered))
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

// startStreamCmd initiates a stream and returns the first event.
// Subsequent events are pulled via waitForEventCmd (command chaining).
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

		// Read first event
		return readOneEvent(ch)
	}
}

// waitForEventCmd reads the next event from the active stream channel.
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

// handleCommand processes slash commands and returns the result text.
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
