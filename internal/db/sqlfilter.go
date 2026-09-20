package db

import (
	"bufio"
	"bytes"
	"io"
	"regexp"
	"strings"
	"unicode"
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
