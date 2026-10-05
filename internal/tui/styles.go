package tui

import (
	"github.com/charmbracelet/lipgloss"
)

var (
	// Brand colors
	ColorPrimary   = lipgloss.Color("#FF5F15") // LAM-Router signature orange
	ColorSecondary = lipgloss.Color("#6366F1") // Indigo
	ColorSuccess   = lipgloss.Color("#10B981") // Emerald
	ColorWarning   = lipgloss.Color("#F59E0B") // Amber
	ColorMuted     = lipgloss.Color("#6B7280") // Grey
	ColorText      = lipgloss.Color("#F9FAFB") // Light white
	ColorDarkBg    = lipgloss.Color("#111216") // Card background
	ColorBorder    = lipgloss.Color("#2D3139") // Border subtle

	// Header Styles
	StyleTitle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorPrimary).
			Padding(0, 1)

	StyleBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#000000")).
			Background(ColorSuccess).
			Padding(0, 1).
			MarginRight(1)

	StyleBadgeMuted = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Background(ColorDarkBg).
			Padding(0, 1).
			MarginRight(1)

	// Tab Styles
	StyleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorText).
			Background(lipgloss.Color("#252830")).
			Border(lipgloss.RoundedBorder(), false, false, false, false).
			Padding(0, 2)

	StyleTabInactive = lipgloss.NewStyle().
				Foreground(ColorMuted).
				Padding(0, 2)

	// Box / Card Styles
	StyleCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorBorder).
			Background(ColorDarkBg).
			Padding(1, 2)

	StyleCardHighlight = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(ColorPrimary).
				Background(ColorDarkBg).
				Padding(1, 2)

	StyleKpiLabel = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Bold(true)

	StyleKpiValue = lipgloss.NewStyle().
			Foreground(ColorText).
			Bold(true).
			MarginTop(1)

	StyleKpiDetail = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF")).
			Italic(true)

	// Table / List Styles
	StyleSelectedItem = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColorPrimary).
				Background(lipgloss.Color("#221D1A")).
				PaddingLeft(1)

	StyleNormalItem = lipgloss.NewStyle().
			Foreground(ColorText).
			PaddingLeft(1)

	StyleMutedItem = lipgloss.NewStyle().
			Foreground(ColorMuted).
			PaddingLeft(1)

	// Footer / Keyhelp
	StyleHelpKey = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorSecondary)

	StyleHelpDesc = lipgloss.NewStyle().
			Foreground(ColorMuted).
			MarginRight(2)
)
