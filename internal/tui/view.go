package tui

import (
	"fmt"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/job"
	"github.com/charmbracelet/lipgloss"
)

var (
	colBg     = lipgloss.Color("#1a1b26")
	colFg     = lipgloss.Color("#c0caf5")
	colMuted  = lipgloss.Color("#565f89")
	colCyan   = lipgloss.Color("#7dcfff")
	colGreen  = lipgloss.Color("#9ece6a")
	colYellow = lipgloss.Color("#e0af68")
	colRed    = lipgloss.Color("#f7768e")
	colBorder = lipgloss.Color("#3b4261")
	colAccent = lipgloss.Color("#bb9af7")
)

func (m model) View() string {
	if !m.ready {
		return "loading lazydbm…"
	}

	header := m.viewHeader()
	footer := m.viewFooter()
	bodyH := max(1, m.height-lipgloss.Height(header)-lipgloss.Height(footer))
	leftW, midW, rightW, paneH := paneSizes(m.width, m.height)
	_ = bodyH

	left := m.viewListPane("connections", m.connLines(), m.connIdx, m.focus == paneConns, leftW, paneH)
	mid := m.viewListPane("files", m.fileLines(), m.fileIdx, m.focus == paneFiles, midW, paneH)
	right := m.viewLogPane(rightW, paneH)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, mid, right)
	screen := lipgloss.JoinVertical(lipgloss.Left, header, body, footer)

	if m.overlay != overlayNone {
		return placeOverlay(screen, m.viewOverlay(), m.width, m.height)
	}
	return screen
}

func (m model) viewHeader() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(colCyan).Render("lazydbm")
	ver := lipgloss.NewStyle().Foreground(colMuted).Render(" " + m.version)
	cwd := lipgloss.NewStyle().Foreground(colMuted).Render("  " + m.cwd)
	line := lipgloss.JoinHorizontal(lipgloss.Center, title, ver, cwd)
	return lipgloss.NewStyle().
		Width(m.width).
		Foreground(colFg).
		Background(lipgloss.Color("#16161e")).
		Padding(0, 1).
		Render(truncate(line, m.width-2))
}

func (m model) viewFooter() string {
	clearLabel := "off"
	if m.clearDB {
		clearLabel = lipgloss.NewStyle().Foreground(colYellow).Bold(true).Render("ON")
	} else {
		clearLabel = lipgloss.NewStyle().Foreground(colMuted).Render("off")
	}
	_, st, _ := m.runner.Snapshot()
	status := st.String()
	stStyle := lipgloss.NewStyle().Foreground(colMuted)
	switch st {
	case job.StatusRunning:
		stStyle = lipgloss.NewStyle().Foreground(colYellow)
	case job.StatusOK:
		stStyle = lipgloss.NewStyle().Foreground(colGreen)
	case job.StatusFail:
		stStyle = lipgloss.NewStyle().Foreground(colRed)
	}

	help := fmt.Sprintf("i import  e export  c clear:%s  p password  r refresh  ? help  q quit", clearLabel)
	left := lipgloss.NewStyle().Foreground(colFg).Render(help)
	right := stStyle.Render("job:" + status)
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right)-2)
	row := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().
		Width(m.width).
		Background(lipgloss.Color("#16161e")).
		Foreground(colFg).
		Padding(0, 1).
		Render(truncate(row, m.width-2))
}

func (m model) connLines() []string {
	out := make([]string, len(m.conns))
	for i, c := range m.conns {
		_, src := m.passwordFor(c)
		badge := "—"
		switch src {
		case "key":
			badge = "key"
		case "env":
			badge = "env"
		case "mem":
			badge = "mem"
		}
		name := c.Name
		if name == "" {
			name = c.Database
		}
		out[i] = fmt.Sprintf("%s  %s  [%s]", c.Short(), name, badge)
	}
	return out
}

func (m model) fileLines() []string {
	if len(m.files) == 0 {
		return nil
	}
	return append([]string(nil), m.files...)
}

func (m model) viewListPane(title string, items []string, selected int, focused bool, width, height int) string {
	border := paneBorder(focused)
	innerW := max(1, width-2)
	innerH := max(1, height-2)

	head := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render(title)
	count := lipgloss.NewStyle().Foreground(colMuted).Render(fmt.Sprintf(" (%d)", len(items)))
	header := truncate(head+count, innerW)

	listH := max(0, innerH-1)
	var body string
	if len(items) == 0 {
		body = lipgloss.NewStyle().Foreground(colMuted).Render("none")
	} else {
		start, end := visibleWindow(len(items), selected, listH)
		var b strings.Builder
		for i := start; i < end; i++ {
			line := items[i]
			cursor := "  "
			style := lipgloss.NewStyle().Foreground(colFg)
			if i == selected {
				cursor = "❯ "
				style = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
				if focused {
					style = style.Background(lipgloss.Color("#1f2335"))
				}
			}
			b.WriteString(style.Render(truncate(cursor+line, innerW)))
			if i < end-1 {
				b.WriteByte('\n')
			}
		}
		body = b.String()
	}

	content := lipgloss.JoinVertical(lipgloss.Left, header, body)
	return border.Width(width).Height(height).Render(content)
}

func (m model) viewLogPane(width, height int) string {
	_, st, _ := m.runner.Snapshot()
	title := "log"
	switch st {
	case job.StatusRunning:
		title = "log · running"
	case job.StatusOK:
		title = "log · success"
	case job.StatusFail:
		title = "log · failed"
	}
	border := paneBorder(m.focus == paneLogs)
	innerW := max(1, width-2)
	innerH := max(1, height-2)
	m.logVP.Width = innerW
	m.logVP.Height = max(1, innerH-1)

	head := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render(truncate(title, innerW))
	content := lipgloss.JoinVertical(lipgloss.Left, head, m.logVP.View())
	return border.Width(width).Height(height).Render(content)
}

func (m model) viewOverlay() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colCyan).
		Background(colBg).
		Foreground(colFg).
		Padding(1, 2).
		Width(min(72, max(40, m.width-8)))

	switch m.overlay {
	case overlayHelp:
		return box.Render(helpText(m.version))
	case overlayNotice:
		return box.Render(m.notice + "\n\n" + muted("enter/esc to close"))
	case overlayPassword:
		c, _ := m.currentConn()
		return box.Render(strings.Join([]string{
			bold("password for " + c.Short()),
			"",
			m.pwInput.View(),
			"",
			muted("enter continue · esc cancel"),
		}, "\n"))
	case overlaySavePassword:
		return box.Render(strings.Join([]string{
			bold("save password to OS keychain?"),
			muted("never written to config.yaml"),
			"",
			"y yes    n this session only",
		}, "\n"))
	case overlayConfirmImport:
		c, _ := m.currentConn()
		f, _ := m.currentFile()
		clear := "NO"
		warn := ""
		if m.clearDB {
			clear = "YES — DROP + CREATE"
			warn = "\n" + lipgloss.NewStyle().Foreground(colRed).Render("this destroys all data in "+c.Database)
		}
		return box.Render(strings.Join([]string{
			bold("import into " + c.Short()),
			"file:  " + f,
			"clear: " + clear + warn,
			"",
			muted("enter confirm · c toggle clear · esc cancel"),
		}, "\n"))
	case overlayExportPath:
		c, _ := m.currentConn()
		return box.Render(strings.Join([]string{
			bold("export " + c.Database),
			"",
			m.pathInput.View(),
			"",
			muted("enter export · esc cancel"),
		}, "\n"))
	default:
		return ""
	}
}

func helpText(version string) string {
	return strings.Join([]string{
		bold("lazydbm " + version),
		"lightweight postgres/mysql import & export",
		"",
		"tab / h l     switch panes",
		"j k / arrows  move",
		"enter         select / import file",
		"i             import",
		"e             export",
		"c             toggle clear-db",
		"p             set / save password",
		"r             refresh discovery",
		"q             quit",
		"ctrl+c        cancel job / quit",
		"",
		"passwords: OS keychain, never in config or logs",
	}, "\n")
}

func paneBorder(focused bool) lipgloss.Style {
	fg := colBorder
	if focused {
		fg = colCyan
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(fg).
		Background(colBg).
		Foreground(colFg)
}

func paneSizes(width, height int) (left, mid, right, paneH int) {
	paneH = max(5, height-2)
	if width < 10 {
		width = 10
	}
	left = max(24, width*34/100)
	mid = max(20, width*28/100)
	if left+mid > width-20 {
		left = width / 3
		mid = width / 3
	}
	right = max(16, width-left-mid)
	return left, mid, right, paneH
}

func visibleWindow(n, selected, height int) (start, end int) {
	if height <= 0 {
		return 0, 0
	}
	if n <= height {
		return 0, n
	}
	start = selected - height/2
	if start < 0 {
		start = 0
	}
	end = start + height
	if end > n {
		end = n
		start = end - height
	}
	if start < 0 {
		start = 0
	}
	return start, end
}

func placeOverlay(screen, overlay string, width, height int) string {
	return overlayOn(screen, overlay, width, height)
}

func overlayOn(screen, overlay string, width, height int) string {
	ow, oh := lipgloss.Size(overlay)
	x := max(0, (width-ow)/2)
	y := max(0, (height-oh)/2)
	sLines := padLines(strings.Split(screen, "\n"), width, height)
	oLines := strings.Split(overlay, "\n")
	for i, line := range oLines {
		row := y + i
		if row < 0 || row >= len(sLines) {
			continue
		}
		sLines[row] = overlayRow(sLines[row], line, x, width)
	}
	return strings.Join(sLines, "\n")
}

func padLines(lines []string, width, height int) []string {
	for i := range lines {
		lines[i] = padRight(lines[i], width)
	}
	for len(lines) < height {
		lines = append(lines, strings.Repeat(" ", width))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return lines
}

func overlayRow(base, overlay string, x, width int) string {
	base = padRight(base, width)
	overlay = strings.TrimRight(overlay, "\n")
	ow := lipgloss.Width(overlay)
	if x < 0 {
		x = 0
	}
	left := cutWidth(base, x)
	right := cutWidthFrom(base, x+ow)
	out := left + overlay + right
	return padRight(truncate(out, width), width)
}

func cutWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		rw := 1
		if n+rw > w {
			break
		}
		b.WriteRune(r)
		n += rw
	}
	return b.String()
}

func cutWidthFrom(s string, start int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if n >= start {
			b.WriteRune(r)
		}
		n++
	}
	return b.String()
}

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return s + strings.Repeat(" ", width-w)
}

func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	return cutWidth(s, width-1) + "…"
}

func bold(s string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(colCyan).Render(s)
}

func muted(s string) string {
	return lipgloss.NewStyle().Foreground(colMuted).Render(s)
}
