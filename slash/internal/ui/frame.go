package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// frame draws a rounded box of exactly width × height cells with a title
// embedded in the top border and an optional label on the right side.
func frame(title, label string, body []string, width, height int, focused bool) string {
	borderColor := colorBorder
	if focused {
		borderColor = colorAccent
	}
	border := lipgloss.NewStyle().Foreground(borderColor)
	innerWidth := width - 2
	innerHeight := height - 2

	titleText := " " + title + " "
	labelText := ""
	if label != "" {
		labelText = " " + label + " "
	}
	ruleWidth := innerWidth - 1 - lipgloss.Width(titleText) - lipgloss.Width(labelText) - 1
	if ruleWidth < 0 {
		labelText = ""
		ruleWidth = max(innerWidth-1-lipgloss.Width(titleText)-1, 0)
		titleText = ansi.Truncate(titleText, innerWidth-2, "… ")
	}

	var b strings.Builder
	b.WriteString(border.Render("╭─"))
	b.WriteString(titleText)
	b.WriteString(border.Render(strings.Repeat("─", ruleWidth)))
	b.WriteString(labelText)
	b.WriteString(border.Render("─╮"))

	side := border.Render("│")
	for i := range innerHeight {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		b.WriteString("\n" + side + fit(line, innerWidth) + side)
	}
	b.WriteString("\n" + border.Render("╰"+strings.Repeat("─", innerWidth)+"╯"))
	return b.String()
}

// overlay draws fg centered on top of bg, keeping bg visible around it.
func overlay(bg, fg string, width, height int) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < height {
		bgLines = append(bgLines, "")
	}
	fgLines := strings.Split(fg, "\n")
	fgWidth := lipgloss.Width(fg)
	x := max((width-fgWidth)/2, 0)
	y := max((height-len(fgLines))/2, 0)

	for i, fgLine := range fgLines {
		row := y + i
		if row >= len(bgLines) {
			break
		}
		bgLine := fit(bgLines[row], width)
		left := ansi.Truncate(bgLine, x, "")
		right := ansi.TruncateLeft(bgLine, x+fgWidth, "")
		bgLines[row] = left + fit(fgLine, fgWidth) + right
	}
	return strings.Join(bgLines, "\n")
}

func dim(s string) string {
	return styleMuted.Faint(true).Render(ansi.Strip(s))
}

// dimAll fades the whole background so a modal stands out.
func dimAll(s string) string {
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = dim(line)
	}
	return strings.Join(lines, "\n")
}
