package ui

import "github.com/charmbracelet/lipgloss"

// Color palette — dark terminal theme
var (
	colorPrimary   = lipgloss.Color("#7C3AED") // Violet
	colorSecondary = lipgloss.Color("#06B6D4") // Cyan
	colorSuccess   = lipgloss.Color("#10B981") // Green
	colorWarning   = lipgloss.Color("#F59E0B") // Amber
	colorError     = lipgloss.Color("#EF4444") // Red
	colorMuted     = lipgloss.Color("#6B7280") // Gray
	colorText      = lipgloss.Color("#E5E7EB") // Light gray
	colorBg        = lipgloss.Color("#111827") // Near black
	colorBorder    = lipgloss.Color("#374151") // Dark gray
	colorUserBg    = lipgloss.Color("#1E1B4B") // Deep violet
	colorAssistBg  = lipgloss.Color("#0F172A") // Deep blue
)

// Styles
var (
	styleApp = lipgloss.NewStyle().
			Background(colorBg)

	styleHeader = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true).
			Padding(0, 1)

	styleStatusBar = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 1).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder)

	styleUserLabel = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true)

	styleAssistLabel = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleUserMsg = lipgloss.NewStyle().
			Padding(0, 2)

	styleAssistMsg = lipgloss.NewStyle().
			Padding(0, 2)

	styleInputBorder = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(colorPrimary).
			Padding(0, 1)

	styleInputPrompt = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleMuted = lipgloss.NewStyle().
		Foreground(colorMuted)

	styleError = lipgloss.NewStyle().
		Foreground(colorError).
		Bold(true)

	styleWarning = lipgloss.NewStyle().
		Foreground(colorWarning).
		Bold(true)
)
