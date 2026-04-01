package ui

import (
	"fmt"
	"strings"
)

// SlashCommand represents a user-invokable command.
type SlashCommand struct {
	Name        string
	Description string
	Handler     func(m *Model, args string) string
}

// RegisterCommands returns the built-in slash commands.
func RegisterCommands() map[string]SlashCommand {
	return map[string]SlashCommand{
		"/help": {
			Name:        "/help",
			Description: "Show available commands",
			Handler:     cmdHelp,
		},
		"/clear": {
			Name:        "/clear",
			Description: "Clear conversation history",
			Handler:     cmdClear,
		},
		"/quit": {
			Name:        "/quit",
			Description: "Exit tc",
			Handler:     nil, // Handled in Update()
		},
		"/exit": {
			Name:        "/exit",
			Description: "Exit tc",
			Handler:     nil,
		},
		"/model": {
			Name:        "/model",
			Description: "Show or switch the active model. Usage: /model [name]",
			Handler:     cmdModel,
		},
		"/cost": {
			Name:        "/cost",
			Description: "Show token usage and estimated cost for this session",
			Handler:     cmdCost,
		},
		"/session": {
			Name:        "/session",
			Description: "Session management. Usage: /session [list|save|load ID]",
			Handler:     cmdSession,
		},
		"/tools": {
			Name:        "/tools",
			Description: "List available tools",
			Handler:     cmdTools,
		},
		"/status": {
			Name:        "/status",
			Description: "Show platform security status and enforcement levels",
			Handler:     cmdStatus,
		},
	}
}

func cmdHelp(m *Model, args string) string {
	var sb strings.Builder
	sb.WriteString("**Available Commands**\n\n")
	sb.WriteString("| Command | Description |\n")
	sb.WriteString("|---------|-------------|\n")

	cmds := RegisterCommands()
	for _, name := range sortedKeys(cmds) {
		cmd := cmds[name]
		sb.WriteString(fmt.Sprintf("| `%s` | %s |\n", cmd.Name, cmd.Description))
	}

	sb.WriteString("\n**Keyboard Shortcuts**\n")
	sb.WriteString("- `Enter` — Send message\n")
	sb.WriteString("- `Ctrl+D` — New line in input\n")
	sb.WriteString("- `Ctrl+C` — Quit\n")

	return sb.String()
}

func cmdClear(m *Model, args string) string {
	m.messages = nil
	m.engine.Reset()
	m.totalInputTokens = 0
	m.totalOutputTokens = 0
	return "Conversation cleared."
}

func cmdModel(m *Model, args string) string {
	if args == "" {
		return fmt.Sprintf("Current model: **%s**", m.modelName)
	}
	m.modelName = strings.TrimSpace(args)
	return fmt.Sprintf("Switched to model: **%s**", m.modelName)
}

func cmdCost(m *Model, args string) string {
	return fmt.Sprintf("**Session Cost**\n\n"+
		"- Input tokens: %d\n"+
		"- Output tokens: %d\n"+
		"- Total tokens: %d\n"+
		"- Messages: %d\n",
		m.totalInputTokens, m.totalOutputTokens,
		m.totalInputTokens+m.totalOutputTokens,
		len(m.messages))
}

func cmdSession(m *Model, args string) string {
	return "Session management: `save`, `list`, `load <id>` (coming soon)"
}

func cmdTools(m *Model, args string) string {
	if m.engine == nil {
		return "No tools registered."
	}

	defs := m.engine.ToolDefinitions()
	if len(defs) == 0 {
		return "No tools registered."
	}

	return fmt.Sprintf("**Registered Tools**\n\n```json\n%s\n```", string(defs))
}

func cmdStatus(m *Model, args string) string {
	return "**Security Status**\n\nRun `tc` with `--verbose` to see enforcement details at startup."
}

func sortedKeys(m map[string]SlashCommand) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Simple sort
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
