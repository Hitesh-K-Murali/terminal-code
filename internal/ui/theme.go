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
	colorDimmer    = lipgloss.Color("#4B5563") // Darker gray
	colorText      = lipgloss.Color("#E5E7EB") // Light gray
	colorBorder    = lipgloss.Color("#374151") // Dark gray
)

// Header bar
var (
	styleHeaderBar = lipgloss.NewStyle().
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	styleHeaderBrand = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleHeaderModel = lipgloss.NewStyle().
			Foreground(colorText)

	styleHeaderMeta = lipgloss.NewStyle().
			Foreground(colorMuted)
)

// Messages
var (
	styleUserDot = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true)

	styleUserLabel = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true)

	styleAssistDot = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleAssistLabel = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleTimestamp = lipgloss.NewStyle().
			Foreground(colorDimmer)

	styleUserMsg = lipgloss.NewStyle().
			Foreground(colorText).
			Padding(0, 2)

	styleAssistMsg = lipgloss.NewStyle().
			Padding(0, 2)

	styleToolCall = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleToolLine = lipgloss.NewStyle().
			Foreground(colorDimmer)

	styleError = lipgloss.NewStyle().
		Foreground(colorError).
		Bold(true)
)

// Spinner / streaming
var (
	styleSpinner = lipgloss.NewStyle().
			Foreground(colorPrimary)

	styleStreamCursor = lipgloss.NewStyle().
				Foreground(colorPrimary).
				Bold(true)

	styleStreamStatus = lipgloss.NewStyle().
				Foreground(colorWarning)
)

// Hint bar
var (
	styleHintBar = lipgloss.NewStyle().
			Foreground(colorMuted).
			BorderTop(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	styleHintKey = lipgloss.NewStyle().
			Foreground(colorText)

	styleHintDesc = lipgloss.NewStyle().
			Foreground(colorDimmer)
)

// Input
var (
	stylePrompt = lipgloss.NewStyle().
			Foreground(colorPrimary).
			Bold(true)

	styleInputArea = lipgloss.NewStyle().
			Padding(0, 1)
)

// General
var (
	styleMuted = lipgloss.NewStyle().
		Foreground(colorMuted)

	styleWarning = lipgloss.NewStyle().
		Foreground(colorWarning).
		Bold(true)
)
