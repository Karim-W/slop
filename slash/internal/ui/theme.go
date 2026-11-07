package ui

import "github.com/charmbracelet/lipgloss"

var (
	colorText      = lipgloss.AdaptiveColor{Light: "#1F2430", Dark: "#D8DEE9"}
	colorMuted     = lipgloss.AdaptiveColor{Light: "#7A8194", Dark: "#6B7385"}
	colorBorder    = lipgloss.AdaptiveColor{Light: "#C9CED9", Dark: "#343B4A"}
	colorAccent    = lipgloss.AdaptiveColor{Light: "#4C63D2", Dark: "#8EA4FF"}
	colorUpload    = lipgloss.AdaptiveColor{Light: "#0F8F7E", Dark: "#5EEAD4"}
	colorWarn      = lipgloss.AdaptiveColor{Light: "#B7791F", Dark: "#FBBF24"}
	colorError     = lipgloss.AdaptiveColor{Light: "#C53030", Dark: "#F87171"}
	colorLink      = lipgloss.AdaptiveColor{Light: "#8B3FD9", Dark: "#C4A1FF"}
	colorCursor    = lipgloss.AdaptiveColor{Light: "#DCE3FF", Dark: "#28304A"}
	colorCursorDim = lipgloss.AdaptiveColor{Light: "#ECEEF3", Dark: "#20242E"}
	colorModalBg   = lipgloss.AdaptiveColor{Light: "#FFFFFF", Dark: "#161A22"}
)

var (
	styleText    = lipgloss.NewStyle().Foreground(colorText)
	styleMuted   = lipgloss.NewStyle().Foreground(colorMuted)
	styleAccent  = lipgloss.NewStyle().Foreground(colorAccent)
	styleBold    = lipgloss.NewStyle().Foreground(colorText).Bold(true)
	styleDir     = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)
	styleLink    = lipgloss.NewStyle().Foreground(colorLink)
	styleMarked  = lipgloss.NewStyle().Foreground(colorWarn)
	styleSuccess = lipgloss.NewStyle().Foreground(colorUpload)
	styleWarn    = lipgloss.NewStyle().Foreground(colorWarn)
	styleError   = lipgloss.NewStyle().Foreground(colorError)
	styleKey     = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	styleBrand = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#0B0E14")).
			Background(colorAccent).
			Bold(true).
			Padding(0, 1)

	styleModal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Background(colorModalBg).
			Padding(1, 2)
)
