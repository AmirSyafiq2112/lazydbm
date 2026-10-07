package db

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
	"unicode"

	"github.com/AmirSyafiq2112/lazydbm/internal/config"
)

var (
	reSchemaAuthOnly = regexp.MustCompile(`(?i)^(\s*CREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?)AUTHORIZATION\s+("(?:[^"]|"")+"|[A-Za-z_][\w$]*|CURRENT_USER|SESSION_USER|CURRENT_ROLE)`)
	reSchemaAuthName = regexp.MustCompile(`(?i)\s+AUTHORIZATION\s+("(?:[^"]|"")+"|[A-Za-z_][\w$]*|CURRENT_USER|SESSION_USER|CURRENT_ROLE)`)
)

func filterPostgresSQL(r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		err := writeFilteredPostgresSQL(pw, r)
		_ = pw.CloseWithError(err)
	}()
	return pr
}

func writeFilteredPostgresSQL(w io.Writer, r io.Reader) error {
	br := bufio.NewReader(r)
	if err := skipBOM(br); err != nil && err != io.EOF {
		return err
	}
	for {
		lead, err := readLeadingSpace(br)
		if len(lead) > 0 && (err == io.EOF || nextIsMeta(br)) {
			if _, werr := w.Write(lead); werr != nil {
				return werr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if nextIsMeta(br) {
			if err := copyMetaLine(w, br); err != nil {
				if err == io.EOF {
					return nil
				}
				return err
			}
			continue
		}
		raw, err := readStatement(br, lead)
		if len(raw) > 0 {
			if werr := emitFilteredStatement(w, raw); werr != nil {
				return werr
			}
			if isCopyFromStdin(raw) {
				cerr := copyCopyData(w, br)
				if cerr != nil && cerr != io.EOF {
					return cerr
				}
				if cerr == io.EOF {
					return nil
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func skipBOM(br *bufio.Reader) error {
	p, err := br.Peek(3)
	if len(p) >= 3 && p[0] == 0xEF && p[1] == 0xBB && p[2] == 0xBF {
		_, _ = br.Discard(3)
	}
	if err != nil && err != io.EOF {
		return err
	}
	return nil
}

func readLeadingSpace(br *bufio.Reader) ([]byte, error) {
	var out []byte
	for {
		p, err := br.Peek(1)
		if len(p) == 0 {
			return out, err
		}
		if p[0] != ' ' && p[0] != '\t' && p[0] != '\n' && p[0] != '\r' {
			return out, nil
		}
		b, err := br.ReadByte()
		if err != nil {
			return out, err
		}
		out = append(out, b)
	}
}

func nextIsMeta(br *bufio.Reader) bool {
	p, _ := br.Peek(1)
	return len(p) == 1 && p[0] == '\\'
}

func copyMetaLine(w io.Writer, br *bufio.Reader) error {
	line, err := br.ReadBytes('\n')
	if len(line) > 0 {
		if _, werr := w.Write(line); werr != nil {
			return werr
		}
	}
	return err
}

func emitFilteredStatement(w io.Writer, raw []byte) error {
	switch classifyPostgresStatement(raw) {
	case stmtDrop:
		return nil
	case stmtRewriteSchema:
		_, err := w.Write([]byte(rewriteCreateSchema(string(raw))))
		return err
	default:
		_, err := w.Write(raw)
		return err
	}
}

type stmtAction int

const (
	stmtKeep stmtAction = iota
	stmtDrop
	stmtRewriteSchema
)

func classifyPostgresStatement(raw []byte) stmtAction {
	compact := unwrapLeadingParens(compactSQL(stripLeadingSQLComments(string(raw))))
	if compact == "" {
		return stmtKeep
	}
	if isDroppedPrivilegeSQL(compact) {
		return stmtDrop
	}
	if strings.HasPrefix(compact, "CREATE SCHEMA") && strings.Contains(compact, " AUTHORIZATION ") {
		return stmtRewriteSchema
	}
	return stmtKeep
}

func unwrapLeadingParens(s string) string {
	for {
		s = strings.TrimSpace(s)
		if !strings.HasPrefix(s, "(") {
			return s
		}
		s = strings.TrimSpace(s[1:])
	}
}

func isDroppedPrivilegeSQL(compact string) bool {
	if privilegePrefix(compact) {
		return true
	}
	if strings.HasPrefix(compact, "ALTER ") && strings.Contains(compact, " OWNER TO ") {
		return true
	}
	if strings.HasPrefix(compact, "WITH ") {
		if i := strings.LastIndex(compact, ")"); i >= 0 {
			return isDroppedPrivilegeSQL(unwrapLeadingParens(compact[i+1:]))
		}
	}
	return false
}

func privilegePrefix(compact string) bool {
	switch {
	case compact == "GRANT" || strings.HasPrefix(compact, "GRANT "):
		return true
	case compact == "REVOKE" || strings.HasPrefix(compact, "REVOKE "):
		return true
	case strings.HasPrefix(compact, "ALTER DEFAULT PRIVILEGES"):
		return true
	case compact == "SET ROLE" || strings.HasPrefix(compact, "SET ROLE "):
		return true
	case strings.HasPrefix(compact, "SET SESSION AUTHORIZATION"):
		return true
	default:
		return false
	}
}

func rewriteCreateSchema(stmt string) string {
	sql := stripLeadingSQLComments(stmt)
	head := stmt[:len(stmt)-len(sql)]
	if reSchemaAuthOnly.MatchString(sql) {
		return head + reSchemaAuthOnly.ReplaceAllString(sql, "$1$2")
	}
	return head + reSchemaAuthName.ReplaceAllString(sql, "")
}

func stripLeadingSQLComments(s string) string {
	for {
		s = strings.TrimLeftFunc(s, unicode.IsSpace)
		switch {
		case strings.HasPrefix(s, "--"):
			if i := strings.IndexAny(s, "\n\r"); i >= 0 {
				s = s[i+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if i := strings.Index(s, "*/"); i >= 0 {
				s = s[i+2:]
				continue
			}
			return ""
		default:
			return s
		}
	}
}

func compactSQL(s string) string {
	return strings.Join(strings.Fields(strings.ToUpper(s)), " ")
}

func isCopyFromStdin(raw []byte) bool {
	compact := compactSQL(stripLeadingSQLComments(string(raw)))
	return strings.HasPrefix(compact, "COPY ") && strings.Contains(compact, " FROM STDIN")
}

// SchemasInPostgresSQL returns schema names from CREATE SCHEMA statements.
// Rows inside COPY ... FROM stdin are ignored. Order follows the file.
func SchemasInPostgresSQL(r io.Reader) ([]string, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	inCopy := false
	var names []string
	seen := map[string]struct{}{}
	for sc.Scan() {
		line := sc.Text()
		if inCopy {
			if strings.TrimRight(line, "\r") == `\.` {
				inCopy = false
			}
			continue
		}
		trim := strings.TrimSpace(line)
		if isCopyFromStdin([]byte(trim)) {
			inCopy = true
			continue
		}
		name, ok := createSchemaName(trim)
		if !ok {
			continue
		}
		if err := config.ValidateSchema(name); err != nil {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

func createSchemaName(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", false
	}
	if !strings.EqualFold(fields[0], "CREATE") || !strings.EqualFold(fields[1], "SCHEMA") {
		return "", false
	}
	i := 2
	if i+2 < len(fields) && strings.EqualFold(fields[i], "IF") && strings.EqualFold(fields[i+1], "NOT") && strings.EqualFold(fields[i+2], "EXISTS") {
		i += 3
	}
	if i >= len(fields) || strings.EqualFold(strings.TrimRight(fields[i], ";"), "AUTHORIZATION") {
		return "", false
	}
	name := strings.TrimRight(fields[i], ";")
	name = unquoteIdent(name)
	if name == "" {
		return "", false
	}
	return name, true
}

func unquoteIdent(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	return strings.ReplaceAll(s[1:len(s)-1], `""`, `"`)
}

// SchemasInFile reads CREATE SCHEMA names from a plain SQL dump.
// Custom-format dumps return no names; the caller clears the selected schema.
func SchemasInFile(path string) ([]string, error) {
	if IsCustomDump(path) {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return SchemasInPostgresSQL(f)
}

// TablesInSQL returns tables named by CREATE TABLE statements.
// Rows inside COPY ... FROM stdin are ignored.
func TablesInSQL(r io.Reader) ([]TableRef, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	inCopy := false
	var tables []TableRef
	seen := map[string]struct{}{}
	for sc.Scan() {
		line := sc.Text()
		if inCopy {
			if strings.TrimRight(line, "\r") == `\.` {
				inCopy = false
			}
			continue
		}
		trim := strings.TrimSpace(line)
		if isCopyFromStdin([]byte(trim)) {
			inCopy = true
			continue
		}
		ref, ok := createTableRef(trim)
		if !ok {
			continue
		}
		if _, exists := seen[ref.Label()]; exists {
			continue
		}
		seen[ref.Label()] = struct{}{}
		tables = append(tables, ref)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return tables, nil
}

// TablesInFile reads CREATE TABLE names from a plain SQL dump.
func TablesInFile(path string) ([]TableRef, error) {
	if IsCustomDump(path) {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return TablesInSQL(f)
}

func createTableRef(line string) (TableRef, bool) {
	fields := strings.Fields(line)
	if len(fields) < 3 || !strings.EqualFold(fields[0], "CREATE") {
		return TableRef{}, false
	}
	i := 1
	for i < len(fields) {
		word := strings.ToUpper(strings.TrimRight(fields[i], ";"))
		switch word {
		case "UNLOGGED", "TEMP", "TEMPORARY", "GLOBAL", "LOCAL":
			i++
			continue
		}
		break
	}
	if i >= len(fields) || !strings.EqualFold(strings.TrimRight(fields[i], ";"), "TABLE") {
		return TableRef{}, false
	}
	i++
	if i+2 < len(fields) && strings.EqualFold(fields[i], "IF") && strings.EqualFold(fields[i+1], "NOT") && strings.EqualFold(fields[i+2], "EXISTS") {
		i += 3
	}
	if i >= len(fields) {
		return TableRef{}, false
	}
	raw := strings.TrimRight(fields[i], ";(")
	schema, name, ok := splitQualified(raw)
	if !ok || config.ValidateTable(name) != nil {
		return TableRef{}, false
	}
	if schema != "" && config.ValidateSchema(schema) != nil {
		return TableRef{}, false
	}
	return TableRef{Schema: schema, Name: name}, true
}

func splitQualified(s string) (schema, name string, ok bool) {
	parts, ok := splitIdents(s)
	if !ok {
		return "", "", false
	}
	switch len(parts) {
	case 1:
		return "", parts[0], parts[0] != ""
	case 2:
		return parts[0], parts[1], parts[0] != "" && parts[1] != ""
	default:
		return "", "", false
	}
}

func splitIdents(s string) ([]string, bool) {
	var out []string
	for i := 0; i < len(s); {
		if s[i] == '.' {
			i++
			continue
		}
		ident, next, ok := readIdent(s, i)
		if !ok {
			return nil, false
		}
		out = append(out, ident)
		i = next
		if i < len(s) && s[i] != '.' {
			return nil, false
		}
	}
	return out, len(out) > 0
}

func readIdent(s string, i int) (string, int, bool) {
	if i >= len(s) {
		return "", i, false
	}
	switch s[i] {
	case '"', '`':
		quote := s[i]
		j := i + 1
		var b strings.Builder
		for j < len(s) {
			if s[j] == quote {
				if j+1 < len(s) && s[j+1] == quote {
					b.WriteByte(quote)
					j += 2
					continue
				}
				return b.String(), j + 1, b.Len() > 0
			}
			b.WriteByte(s[j])
			j++
		}
		return "", i, false
	default:
		j := i
		for j < len(s) && s[j] != '.' {
			j++
		}
		if j == i {
			return "", i, false
		}
		return s[i:j], j, true
	}
}

const (
	stNormal = iota
	stSQuote
	stDQuote
	stLineComment
	stBlockComment
	stDollarBody
)

func readStatement(br *bufio.Reader, lead []byte) ([]byte, error) {
	stmt := bytes.NewBuffer(lead)
	state := stNormal
	blockDepth := 0
	dollarTag := ""
	dollarMin := 0
	for {
		b, err := br.ReadByte()
		if err != nil {
			return stmt.Bytes(), err
		}
		switch state {
		case stNormal:
			if err := stmt.WriteByte(b); err != nil {
				return nil, err
			}
			switch b {
			case ';':
				return stmt.Bytes(), nil
			case '\'':
				state = stSQuote
			case '"':
				state = stDQuote
			case '-':
				if n, _ := br.Peek(1); len(n) == 1 && n[0] == '-' {
					state = stLineComment
				}
			case '/':
				if n, _ := br.Peek(1); len(n) == 1 && n[0] == '*' {
					state = stBlockComment
					blockDepth = 1
				}
			case '$':
				tag, ok, err := readDollarTag(br)
				if err != nil {
					return stmt.Bytes(), err
				}
				if ok {
					if _, err := stmt.WriteString(tag); err != nil {
						return nil, err
					}
					dollarTag = "$" + tag
					dollarMin = stmt.Len() + len(dollarTag)
					state = stDollarBody
				}
			}
		case stSQuote:
			if err := stmt.WriteByte(b); err != nil {
				return nil, err
			}
			if b == '\'' {
				if n, _ := br.Peek(1); len(n) == 1 && n[0] == '\'' {
					nb, _ := br.ReadByte()
					if err := stmt.WriteByte(nb); err != nil {
						return nil, err
					}
					continue
				}
				state = stNormal
			}
		case stDQuote:
			if err := stmt.WriteByte(b); err != nil {
				return nil, err
			}
			if b == '"' {
				if n, _ := br.Peek(1); len(n) == 1 && n[0] == '"' {
					nb, _ := br.ReadByte()
					if err := stmt.WriteByte(nb); err != nil {
						return nil, err
					}
					continue
				}
				state = stNormal
			}
		case stLineComment:
			if err := stmt.WriteByte(b); err != nil {
				return nil, err
			}
			if b == '\n' {
				state = stNormal
			}
		case stBlockComment:
			if err := stmt.WriteByte(b); err != nil {
				return nil, err
			}
			if b == '/' {
				if n, _ := br.Peek(1); len(n) == 1 && n[0] == '*' {
					nb, _ := br.ReadByte()
					if err := stmt.WriteByte(nb); err != nil {
						return nil, err
					}
					blockDepth++
				}
			} else if b == '*' {
				if n, _ := br.Peek(1); len(n) == 1 && n[0] == '/' {
					nb, _ := br.ReadByte()
					if err := stmt.WriteByte(nb); err != nil {
						return nil, err
					}
					blockDepth--
					if blockDepth <= 0 {
						state = stNormal
					}
				}
			}
		case stDollarBody:
			if err := stmt.WriteByte(b); err != nil {
				return nil, err
			}
			if stmt.Len() >= dollarMin && bytes.HasSuffix(stmt.Bytes(), []byte(dollarTag)) {
				state = stNormal
			}
		}
	}
}

func readDollarTag(br *bufio.Reader) (string, bool, error) {
	p, err := br.Peek(1)
	if len(p) == 0 {
		return "", false, err
	}
	if p[0] == '$' {
		_, _ = br.ReadByte()
		return "$", true, nil
	}
	if !isDollarTagStart(p[0]) {
		return "", false, nil
	}
	var tag strings.Builder
	for {
		p, err := br.Peek(1)
		if len(p) == 0 {
			return "", false, err
		}
		c := p[0]
		if c == '$' {
			_, _ = br.ReadByte()
			tag.WriteByte('$')
			return tag.String(), true, nil
		}
		if !isDollarTagChar(c) {
			return "", false, nil
		}
		_, _ = br.ReadByte()
		tag.WriteByte(c)
	}
}

func isDollarTagStart(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func isDollarTagChar(b byte) bool {
	return isDollarTagStart(b) || (b >= '0' && b <= '9')
}

func copyCopyData(w io.Writer, br *bufio.Reader) error {
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				return werr
			}
		}
		if isCopyEnd(line) {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if err != nil {
			return err
		}
	}
}

func isCopyEnd(line []byte) bool {
	s := bytes.TrimRight(line, "\r\n")
	return bytes.Equal(s, []byte(`\.`))
}
