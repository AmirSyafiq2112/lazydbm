package tui

import (
	"fmt"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/job"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// ANSI 0–15 so colors come from the terminal theme, like lazygit.
// No hex and no custom backgrounds — the terminal background shows through.
var (
	colMuted  = lipgloss.Color("8") // bright black / theme gray
	colCyan   = lipgloss.Color("6")
	colGreen  = lipgloss.Color("2")
	colYellow = lipgloss.Color("3")
	colRed    = lipgloss.Color("1")
	colAccent = lipgloss.Color("6")
	colBorder = lipgloss.Color("8")
)

func (m model) View() string {
	if !m.ready {
		return "loading lazydbm…"
	}
	m.syncLogs()

	header := m.viewHeader()
	footer := m.viewFooter()
	bodyH := max(1, m.height-lipgloss.Height(header)-lipgloss.Height(footer))
	leftW, midW, rightW, topH, logH := paneSizes(m.width, bodyH)

	left := m.viewListPane("servers", m.connLines(), m.connIdx, m.focus == paneServers, leftW, topH, "none")
	mid := m.viewDBPane(midW, topH)
	right := m.viewListPane("files", m.fileLines(), m.fileIdx, m.focus == paneFiles, rightW, topH, "none")
	top := lipgloss.JoinHorizontal(lipgloss.Top, left, mid, right)
	log := m.viewLogPane(m.width, logH)

	body := lipgloss.JoinVertical(lipgloss.Left, top, log)
	screen := clipTo(lipgloss.JoinVertical(lipgloss.Left, header, body, footer), m.width, m.height)

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

	help := fmt.Sprintf("enter connect  i import  e export  n new-db  a add  E edit  d delete  c clear:%s  p password  r refresh  q quit", clearLabel)
	left := help
	right := stStyle.Render("job:" + status)
	gap := max(1, m.width-lipgloss.Width(left)-lipgloss.Width(right)-2)
	row := left + strings.Repeat(" ", gap) + right
	return lipgloss.NewStyle().
		Width(m.width).
		Padding(0, 1).
		Render(truncate(row, m.width-2))
}

func (m model) connLines() []string {
	out := make([]string, len(m.conns))
	for i, c := range m.conns {
		_, src := m.passwordFor(c)
		origin := "env"
		switch {
		case c.Saved():
			origin = "saved"
		case strings.Contains(strings.ToLower(c.Source), "compose"):
			origin = "compose"
		case src == "key":
			origin = "key"
		case src == "mem":
			origin = "mem"
		case c.Source != "" && !strings.HasPrefix(c.Source, ".env"):
			origin = c.Source
		}
		name := c.Name
		if name == "" {
			name = c.User
		}
		out[i] = fmt.Sprintf("%s  %s  [%s]", c.Short(), name, origin)
	}
	return out
}

func (m model) fileLines() []string {
	if len(m.files) == 0 {
		return nil
	}
	return append([]string(nil), m.files...)
}

func (m model) viewDBPane(width, height int) string {
	if m.dbErr != "" {
		return m.viewListPane("databases", nil, 0, m.focus == paneDBs, width, height, m.dbErr)
	}
	if !m.dbReady {
		return m.viewListPane("databases", nil, 0, m.focus == paneDBs, width, height, "enter to connect")
	}
	return m.viewListPane("databases", m.databases, m.dbIdx, m.focus == paneDBs, width, height, "none")
}

func (m model) viewListPane(title string, items []string, selected int, focused bool, width, height int, emptyHint string) string {
	border := paneBorder(focused)
	innerW := max(1, width-border.GetHorizontalFrameSize())
	innerH := max(1, height-border.GetVerticalFrameSize())

	head := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render(title)
	count := lipgloss.NewStyle().Foreground(colMuted).Render(fmt.Sprintf(" (%d)", len(items)))
	header := truncate(head+count, innerW)

	listH := max(0, innerH-1)
	var body string
	if len(items) == 0 {
		hint := emptyHint
		if hint == "" {
			hint = "none"
		}
		style := lipgloss.NewStyle().Foreground(colMuted)
		if m.dbErr != "" && title == "databases" {
			style = lipgloss.NewStyle().Foreground(colRed)
		}
		body = style.Render(truncate(hint, innerW))
	} else {
		start, end := visibleWindow(len(items), selected, listH)
		var b strings.Builder
		for i := start; i < end; i++ {
			line := items[i]
			cursor := "  "
			style := lipgloss.NewStyle()
			if i == selected {
				cursor = "❯ "
				style = lipgloss.NewStyle().Bold(true).Foreground(colGreen)
				if focused {
					style = lipgloss.NewStyle().Bold(true).Reverse(true)
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
	return border.Width(innerW).Height(innerH).MaxWidth(width).MaxHeight(height).Render(content)
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
	innerW := max(1, width-border.GetHorizontalFrameSize())
	innerH := max(1, height-border.GetVerticalFrameSize())
	m.logVP.Width = innerW
	m.logVP.Height = max(1, innerH-1)

	head := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render(truncate(title, innerW))
	content := lipgloss.JoinVertical(lipgloss.Left, head, m.logVP.View())
	return border.Width(innerW).Height(innerH).MaxWidth(width).MaxHeight(height).Render(content)
}

func (m model) viewOverlay() string {
	inner := min(70, max(38, m.width-10))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colGreen).
		Padding(1, 2).
		Width(inner)

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
			muted("enter continue · empty = trust / no password · esc cancel"),
		}, "\n"))
	case overlaySavePassword:
		if strings.TrimSpace(m.pwInput.Value()) == "" {
			return box.Render(strings.Join([]string{
				bold("remember no-password (trust) for this server?"),
				muted("writes no_password in config.yaml, never a secret"),
				"",
				"y yes    n this session only",
			}, "\n"))
		}
		return box.Render(strings.Join([]string{
			bold("save password to OS keychain?"),
			muted("never written to config.yaml"),
			"",
			"y yes    n this session only",
		}, "\n"))
	case overlayConfirmImport:
		c, _ := m.currentConn()
		dbName, _ := m.currentDB()
		f, _ := m.currentFile()
		clear := "NO"
		warn := ""
		if m.clearDB {
			clear = "YES — DROP + CREATE"
			warn = "\n" + lipgloss.NewStyle().Foreground(colRed).Render("this destroys all data in "+dbName)
		}
		return box.Render(strings.Join([]string{
			bold("import into " + dbName + " on " + c.Short()),
			"file:  " + f,
			"clear: " + clear + warn,
			"",
			muted("enter confirm · c toggle clear · esc cancel"),
		}, "\n"))
	case overlayExportPath:
		dbName, _ := m.currentDB()
		return box.Render(strings.Join([]string{
			bold("export " + dbName),
			"",
			m.pathInput.View(),
			"",
			muted("enter export · esc cancel"),
		}, "\n"))
	case overlayCreateDB:
		c, _ := m.currentConn()
		return box.Render(strings.Join([]string{
			bold("create database on " + c.Short()),
			muted("created if missing, then selected for import/export"),
			"",
			m.dbInput.View(),
			"",
			muted("enter create · esc cancel"),
		}, "\n"))
	case overlayAdd, overlayEdit:
		return box.Width(min(76, max(46, m.width-10))).Render(m.viewConnForm())
	case overlayConfirmDelete:
		c, _ := m.currentConn()
		return box.Render(strings.Join([]string{
			bold("remove saved server?"),
			c.Short(),
			"",
			muted("enter confirm · esc cancel"),
			muted("keychain password is removed too"),
		}, "\n"))
	default:
		return ""
	}
}

func (m model) viewConnForm() string {
	title := "add server"
	if m.overlay == overlayEdit {
		title = "edit server"
		for _, c := range m.conns {
			if c.ID() == m.formEditID && !c.Saved() {
				title = "save as server"
				break
			}
		}
	}
	mark := func(i int) string {
		if m.formFocus == i {
			return "❯ "
		}
		return "  "
	}
	engine := string(m.formEngine)
	if m.formFocus == formEngine {
		engine = engine + "  (space/p/m)"
	}
	save := "no"
	if m.formSaveKey {
		save = "yes"
	}
	lines := []string{
		bold(title),
		muted("tab next field · enter save · esc cancel"),
		"",
		mark(formEngine) + "engine    " + engine,
		mark(formName) + "name      " + m.formInputs[idxName].View(),
		mark(formHost) + "host      " + m.formInputs[idxHost].View(),
		mark(formPort) + "port      " + m.formInputs[idxPort].View(),
		mark(formUser) + "user      " + m.formInputs[idxUser].View(),
	}
	if m.formErr != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(colRed).Render(m.formErr))
	}
	lines = append(lines,
		mark(formPass)+"password  "+m.formInputs[idxPass].View(),
		mark(formSaveKey)+"keychain  "+save+"  (y/n)",
		"",
		muted("empty password = trust / no password; never written to config.yaml"),
	)
	return strings.Join(lines, "\n")
}

func helpText(version string) string {
	return strings.Join([]string{
		bold("lazydbm " + version),
		"lightweight postgres/mysql import & export",
		"",
		"tab / h l     servers → databases → files",
		"j k / arrows  move in focused pane",
		"enter         connect server / import file",
		"i             import selected file into selected database",
		"e             export selected database",
		"n             create a database on the server if it is missing",
		"a             add server",
		"E             edit / save as server",
		"d             delete saved server",
		"c             toggle clear-db (import only)",
		"p             set / save password",
		"r             re-list databases and refresh files",
		"q             quit",
		"ctrl+c        cancel job / quit",
		"",
		"passwords: OS keychain, never in config or logs",
		"empty password is allowed (local trust / peer auth)",
		"import creates the target database if it does not exist",
	}, "\n")
}

func paneBorder(focused bool) lipgloss.Style {
	fg := colBorder
	if focused {
		fg = colGreen
	}
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(fg)
}

func paneSizes(width, bodyH int) (left, mid, right, topH, logH int) {
	if width < 9 {
		width = 9
	}
	if bodyH < 6 {
		bodyH = 6
	}
	logH = max(5, bodyH*32/100)
	if logH > bodyH-6 {
		logH = max(3, bodyH-6)
	}
	topH = max(3, bodyH-logH)
	if topH+logH != bodyH {
		logH = bodyH - topH
	}
	if logH < 3 {
		logH = 3
		topH = max(3, bodyH-logH)
	}

	left = max(8, width*34/100)
	mid = max(8, width*33/100)
	if left+mid > width-8 {
		left = width / 3
		mid = width / 3
	}
	right = width - left - mid
	if right < 0 {
		right = 0
		left = width / 2
		mid = width - left
	}
	if left+mid+right != width {
		right = width - left - mid
	}
	return left, mid, right, topH, logH
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

func clipTo(s string, width, height int) string {
	return strings.Join(padLines(strings.Split(s, "\n"), width, height), "\n")
}

func padLines(lines []string, width, height int) []string {
	for i := range lines {
		lines[i] = padRight(truncate(lines[i], width), width)
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
	base = padRight(truncate(base, width), width)
	overlay = strings.TrimRight(overlay, "\n")
	ow := lipgloss.Width(overlay)
	if x < 0 {
		x = 0
	}
	if x > width {
		x = width
	}
	if x+ow > width {
		overlay = truncate(overlay, width-x)
		ow = lipgloss.Width(overlay)
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
	return ansi.Truncate(s, w, "")
}

func cutWidthFrom(s string, start int) string {
	if start <= 0 {
		return s
	}
	return ansi.TruncateLeft(s, start, "")
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
	return ansi.Truncate(s, width, "…")
}

func bold(s string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(colCyan).Render(s)
}

func muted(s string) string {
	return lipgloss.NewStyle().Foreground(colMuted).Render(s)
}
