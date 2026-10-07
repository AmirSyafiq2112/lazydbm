package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AmirSyafiq2112/lazydbm/internal/db"
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

	var body string
	if m.logFull {
		body = m.viewLogPane(m.width, bodyH)
	} else {
		left := m.viewListPane("servers", m.connLines(), nil, m.connIdx, m.focus == paneServers, leftW, topH, "none")
		mid := m.viewDBPane(midW, topH)
		right := m.viewFilesPane(rightW, topH)
		top := lipgloss.JoinHorizontal(lipgloss.Top, left, mid, right)
		log := m.viewLogPane(m.width, logH)
		body = lipgloss.JoinVertical(lipgloss.Left, top, log)
	}
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

	clearKey := "clear"
	if m.tableMode {
		clearKey = "table"
	} else if m.schemaMode {
		clearKey = "schema"
	}
	help := fmt.Sprintf("enter connect  i import  e export  n new-db  a add  E edit  d delete  c %s:%s  p password  r refresh  f log  / find  q quit", clearKey, clearLabel)
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
	return m.visibleFiles()
}

func (m model) viewFilesPane(width, height int) string {
	items := m.visibleFiles()
	title := "files"
	if m.fileSearch || m.fileQuery != "" {
		mark := ""
		if m.fileSearch {
			mark = "_"
		}
		title = "files /" + m.fileQuery + mark
	}
	empty := "none"
	if m.fileQuery != "" && len(items) == 0 {
		empty = "no match"
	}
	return m.viewListPane(title, items, m.fileDates(items), m.fileIdx, m.focus == paneFiles, width, height, empty)
}

func (m model) fileDates(paths []string) []string {
	out := make([]string, len(paths))
	for i, rel := range paths {
		info, err := os.Stat(filepath.Join(m.cwd, rel))
		if err != nil || info.IsDir() {
			continue
		}
		out[i] = info.ModTime().Format("02 Jan 15:04")
	}
	return out
}

func (m model) viewDBPane(width, height int) string {
	if m.dbErr != "" {
		return m.viewListPane("databases", nil, nil, 0, m.focus == paneDBs, width, height, m.dbErr)
	}
	if !m.dbReady {
		return m.viewListPane("databases", nil, nil, 0, m.focus == paneDBs, width, height, "enter to connect")
	}
	if m.tableMode {
		title := "tables"
		if m.tableDB != "" {
			title = "tables · " + m.tableDB
		}
		if m.tableSchema != "" {
			title = "tables · " + m.tableDB + "." + m.tableSchema
		}
		if m.tableSearch || m.tableQuery != "" {
			mark := ""
			if m.tableSearch {
				mark = "_"
			}
			title += " /" + m.tableQuery + mark
		}
		empty := "no tables"
		if m.tableQuery != "" && len(m.visibleTables()) == 0 {
			empty = "no match"
		}
		return m.viewListPane(title, m.visibleTables(), nil, m.tableIdx, m.focus == paneDBs, width, height, empty)
	}
	if m.schemaMode {
		title := "schemas"
		if m.schemaDB != "" {
			title = "schemas · " + m.schemaDB
		}
		if m.dbSearch || m.dbQuery != "" {
			mark := ""
			if m.dbSearch {
				mark = "_"
			}
			title += " /" + m.dbQuery + mark
		}
		empty := "no schemas"
		if m.dbQuery != "" && len(m.visibleSchemas()) == 0 {
			empty = "no match"
		}
		return m.viewListPane(title, m.visibleSchemas(), nil, m.schemaIdx, m.focus == paneDBs, width, height, empty)
	}
	title := "databases"
	if m.dbSearch || m.dbQuery != "" {
		mark := ""
		if m.dbSearch {
			mark = "_"
		}
		title = "databases /" + m.dbQuery + mark
	}
	empty := "none"
	if m.dbQuery != "" && len(m.visibleDatabases()) == 0 {
		empty = "no match"
	}
	return m.viewListPane(title, m.databaseLines(), nil, m.dbIdx, m.focus == paneDBs, width, height, empty)
}

func (m model) viewListPane(title string, items, suffixes []string, selected int, focused bool, width, height int, emptyHint string) string {
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
			suffix := ""
			if i < len(suffixes) {
				suffix = suffixes[i]
			}
			if suffix == "" {
				b.WriteString(style.Render(truncate(cursor+line, innerW)))
			} else {
				room := innerW - lipgloss.Width(cursor) - lipgloss.Width(suffix) - 2
				if room < 1 {
					room = 1
				}
				name := truncate(line, room)
				pad := innerW - lipgloss.Width(cursor) - lipgloss.Width(name) - lipgloss.Width(suffix)
				if pad < 1 {
					pad = 1
				}
				b.WriteString(style.Render(cursor + name + strings.Repeat(" ", pad) + suffix))
			}
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
	if m.logFull {
		title += " · full"
	}
	if pct := m.logScrollLabel(); pct != "" {
		title += " · " + pct
	}
	border := paneBorder(m.focus == paneLogs)
	innerW := max(1, width-border.GetHorizontalFrameSize())
	innerH := max(1, height-border.GetVerticalFrameSize())
	m.logVP.Width = innerW
	m.logVP.Height = max(1, innerH-1)

	hint := title
	if m.logFull {
		hint += "   j/k scroll · g/G top/bottom · esc close"
	}
	head := lipgloss.NewStyle().Foreground(colAccent).Bold(true).Render(truncate(hint, innerW))
	content := lipgloss.JoinVertical(lipgloss.Left, head, m.logVP.View())
	return border.Width(innerW).Height(innerH).MaxWidth(width).MaxHeight(height).Render(content)
}

func (m model) logScrollLabel() string {
	total := m.logVP.TotalLineCount()
	if total <= m.logVP.Height || m.logVP.Height <= 0 {
		return ""
	}
	return fmt.Sprintf("%d%%", int(m.logVP.ScrollPercent()*100))
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
		if m.schemaDB != "" {
			dbName = m.schemaDB
		}
		schema, _ := m.currentSchema()
		table, _ := m.currentTable()
		clearNames := m.importSchemas
		if schema != "" && !m.tableMode && len(clearNames) == 0 {
			clearNames = []string{schema}
		}
		tableNames := make([]string, len(m.importTables))
		for i, item := range m.importTables {
			tableNames[i] = item.Label()
		}
		if m.tableMode && len(tableNames) == 0 && table != "" {
			if schema != "" {
				tableNames = []string{schema + "." + table}
			} else {
				tableNames = []string{table}
			}
		}
		target := dbName
		if m.tableMode && len(tableNames) == 1 {
			target = dbName + "." + tableNames[0]
		} else if schema != "" && !m.tableMode && len(m.importSchemas) <= 1 && len(clearNames) == 1 {
			target = dbName + "." + clearNames[0]
		}
		f, _ := m.currentFile()
		clear := "NO"
		warn := ""
		if m.clearDB && m.tableMode && len(tableNames) > 0 {
			if len(tableNames) == 1 {
				clear = "YES — DROP TABLE " + tableNames[0]
				warn = "\n" + lipgloss.NewStyle().Foreground(colRed).Render("this destroys "+tableNames[0])
			} else {
				clear = fmt.Sprintf("YES — DROP %d TABLES\n  %s", len(tableNames), strings.Join(tableNames, "\n  "))
				warn = "\n" + lipgloss.NewStyle().Foreground(colRed).Render("this destroys those tables")
			}
			warn += "\n" + lipgloss.NewStyle().Foreground(colRed).Render("dependent objects in other tables are dropped too")
		} else if m.clearDB && len(clearNames) > 0 && schema != "" {
			if len(clearNames) == 1 {
				clear = "YES — DROP SCHEMA " + clearNames[0]
				warn = "\n" + lipgloss.NewStyle().Foreground(colRed).Render("this destroys all objects in schema "+clearNames[0])
			} else {
				clear = fmt.Sprintf("YES — DROP %d SCHEMAS\n  %s", len(clearNames), strings.Join(clearNames, "\n  "))
				warn = "\n" + lipgloss.NewStyle().Foreground(colRed).Render("this destroys all objects in those schemas")
			}
			warn += "\n" + lipgloss.NewStyle().Foreground(colRed).Render("dependent objects in other schemas are dropped too")
		} else if m.clearDB {
			clear = "YES — DROP + CREATE"
			warn = "\n" + lipgloss.NewStyle().Foreground(colRed).Render("this destroys all data in "+dbName)
		}
		return box.Render(strings.Join([]string{
			bold("import into " + target + " on " + c.Short()),
			"file:  " + f,
			"clear: " + clear + warn,
			"",
			muted("enter confirm · c toggle clear · esc cancel"),
		}, "\n"))
	case overlayExportPath:
		dbName, _ := m.currentDB()
		if m.schemaDB != "" {
			dbName = m.schemaDB
		}
		title := "export " + dbName
		hint := "enter export · esc cancel"
		if m.exportSchema != "" {
			title = "export " + dbName + "." + m.exportSchema
			hint = "this schema only · enter export · esc cancel"
		}
		return box.Render(strings.Join([]string{
			bold(title),
			"",
			m.pathInput.View(),
			"",
			muted(hint),
		}, "\n"))
	case overlayExportPreview:
		dbName, _ := m.currentDB()
		if m.tableDB != "" {
			dbName = m.tableDB
		} else if m.schemaDB != "" {
			dbName = m.schemaDB
		}
		if len(m.exportTables) > 0 {
			return box.Render(m.exportTablePreviewText(dbName))
		}
		return box.Render(m.exportPreviewText(dbName))
	case overlayProtectedImport:
		c, _ := m.currentConn()
		name := m.importDatabaseName()
		lines := []string{
			bold("protected database"),
			"",
			"import into " + name,
			c.Short(),
			"",
			"type the database name to continue",
			"",
			m.dbInput.View(),
		}
		if m.guardErr != "" {
			lines = append(lines, lipgloss.NewStyle().Foreground(colRed).Render(m.guardErr))
		}
		lines = append(lines, "", muted("esc cancel"))
		return box.Render(strings.Join(lines, "\n"))
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

func (m model) exportTablePreviewText(dbName string) string {
	plan := m.exportTables
	total := int64(0)
	for _, table := range plan {
		total += table.Bytes
	}
	limit := len(plan)
	if m.height >= 24 && len(plan) > m.height-16 {
		limit = m.height - 16
		if limit < 6 {
			limit = 6
		}
	}
	shown := plan
	extra := 0
	if len(plan) > limit {
		shown = plan[:limit]
		extra = len(plan) - limit
	}
	nameWidth := 0
	for _, table := range shown {
		if len(table.Label()) > nameWidth {
			nameWidth = len(table.Label())
		}
	}
	head := dbName
	if len(plan) > 0 {
		head = dbName + "." + plan[0].Label()
	}
	lines := []string{bold("export " + head), ""}
	if len(plan) <= 1 {
		lines = append(lines, "no foreign keys point outside this table", "")
	} else {
		lines = append(lines, "these tables will be exported", "")
	}
	for i, table := range shown {
		mark := "  "
		if i == 0 {
			mark = "❯ "
		}
		lines = append(lines, fmt.Sprintf("%s%-*s  %s", mark, nameWidth, table.Label(), db.FormatBytes(table.Bytes)))
	}
	if extra > 0 {
		lines = append(lines, fmt.Sprintf("  … and %d more", extra))
	}
	label := "1 table"
	if len(plan) != 1 {
		label = fmt.Sprintf("%d tables", len(plan))
	}
	lines = append(lines,
		"",
		label+" · "+db.FormatBytes(total),
		"",
		m.pathInput.View(),
		"",
		muted("enter export · esc cancel"),
	)
	return strings.Join(lines, "\n")
}

func (m model) exportPreviewText(dbName string) string {
	plan := m.exportPlan
	total := int64(0)
	for _, s := range plan {
		total += s.Bytes
	}
	limit := len(plan)
	if m.height >= 24 && len(plan) > m.height-16 {
		limit = m.height - 16
		if limit < 6 {
			limit = 6
		}
	}
	shown := plan
	extra := 0
	if len(plan) > limit {
		shown = plan[:limit]
		extra = len(plan) - limit
	}
	nameWidth := 0
	for _, s := range shown {
		if len(s.Name) > nameWidth {
			nameWidth = len(s.Name)
		}
	}
	lines := []string{bold("export " + dbName + "." + m.exportSchema), ""}
	if len(plan) <= 1 {
		lines = append(lines, "no foreign keys point outside this schema", "")
	} else {
		lines = append(lines, "these schemas will be exported", "")
	}
	for _, s := range shown {
		mark := "  "
		if s.Name == m.exportSchema {
			mark = "❯ "
		}
		lines = append(lines, fmt.Sprintf("%s%-*s  %s", mark, nameWidth, s.Name, db.FormatBytes(s.Bytes)))
	}
	if extra > 0 {
		lines = append(lines, fmt.Sprintf("  … and %d more", extra))
	}
	label := "1 schema"
	if len(plan) != 1 {
		label = fmt.Sprintf("%d schemas", len(plan))
	}
	lines = append(lines,
		"",
		label+" · "+db.FormatBytes(total),
		"",
		m.pathInput.View(),
		"",
		muted("enter export · esc cancel"),
	)
	return strings.Join(lines, "\n")
}

func helpText(version string) string {
	return strings.Join([]string{
		bold("lazydbm " + version),
		"lightweight postgres/mysql import & export",
		"",
		"tab / h l     servers → databases → files → log",
		"j k / arrows  move in focused pane",
		"f             full-screen log (j/k scroll, esc closes)",
		"/             search the focused list (files, databases, schemas, tables)",
		"enter         connect / list schemas / list tables / import file",
		"backspace     up one level: tables, schemas, then databases",
		"i             import selected file into selected database",
		"e             export database, schema, or table after a size preview",
		"P             mark the selected database protected",
		"n             create a database on the server if it is missing",
		"a             add server",
		"E             edit / save as server",
		"d             delete saved server",
		"c             toggle clear before import",
		"              database list: drop that database",
		"              schema list: drop schemas named in the file",
		"              table list: drop tables named in the file",
		"p             set / save password",
		"r             refresh sql files and re-list databases",
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
