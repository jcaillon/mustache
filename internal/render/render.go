package render

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

type MissingBehavior uint8

const (
	MissingError MissingBehavior = iota
	MissingEmpty
	MissingKeep
)

type Options struct {
	Missing    MissingBehavior
	Partials   map[string]string
	Delimiters [2]string
}

type Renderer struct {
	options Options
}

type Error struct {
	Line int
	Err  error
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d: %v", e.Line, e.Err)
}

func (e *Error) Unwrap() error {
	return e.Err
}

func New(options Options) (*Renderer, error) {
	if options.Delimiters == [2]string{} {
		options.Delimiters = [2]string{"{{", "}}"}
	}
	if err := validateDelimiters(options.Delimiters[0], options.Delimiters[1]); err != nil {
		return nil, err
	}
	return &Renderer{options: options}, nil
}

func (r *Renderer) Render(w io.Writer, template io.Reader, templatePath string, data any) error {
	if templatePath != "" {
		var err error
		templatePath, err = filepath.Abs(templatePath)
		if err != nil {
			return fmt.Errorf("resolve template path: %w", err)
		}
	}
	l := newLexer(template, r.options.Delimiters[0], r.options.Delimiters[1])
	return r.renderStream(w, l, []any{data}, templatePath)
}

func (r *Renderer) renderStream(w io.Writer, l *lexer, stack []any, templatePath string) error {
	for {
		tok, err := l.next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		switch tok.kind {
		case tokenText:
			if _, err := io.WriteString(w, tok.raw); err != nil {
				return err
			}
		case tokenVariable, tokenUnescaped:
			if err := r.renderVariable(w, tok, stack); err != nil {
				return err
			}
		case tokenSection, tokenInverted:
			node, err := parseSection(l, tok)
			if err != nil {
				return err
			}
			if err := r.renderSection(w, node, stack, templatePath); err != nil {
				return err
			}
		case tokenEnd:
			return fmt.Errorf("unexpected closing section %q", tok.name)
		case tokenPartial:
			if err := r.renderPartial(w, tok, stack, templatePath); err != nil {
				return err
			}
		}
	}
}

func (r *Renderer) renderVariable(w io.Writer, tok token, stack []any) error {
	value, found := lookup(tok.name, stack)
	if !found {
		switch r.options.Missing {
		case MissingEmpty:
			return nil
		case MissingKeep:
			_, err := io.WriteString(w, tok.raw)
			return err
		default:
			return &Error{Line: tok.line, Err: fmt.Errorf("missing tag %q", tok.name)}
		}
	}

	text := stringify(value)
	if tok.kind == tokenVariable {
		text = escapeHTML(text)
	}
	_, err := io.WriteString(w, text)
	return err
}

type node struct {
	token    token
	children []node
	closeRaw string
}

func parseSection(l *lexer, opener token) (node, error) {
	n := node{token: opener}
	for {
		tok, err := l.next()
		if errors.Is(err, io.EOF) {
			return node{}, fmt.Errorf("unclosed section %q", opener.name)
		}
		if err != nil {
			return node{}, err
		}
		if tok.kind == tokenEnd {
			if tok.name != opener.name {
				return node{}, fmt.Errorf("section %q closed by %q", opener.name, tok.name)
			}
			n.closeRaw = tok.raw
			return n, nil
		}
		if tok.kind == tokenSection || tok.kind == tokenInverted {
			child, err := parseSection(l, tok)
			if err != nil {
				return node{}, err
			}
			n.children = append(n.children, child)
			continue
		}
		n.children = append(n.children, node{token: tok})
	}
}

func (r *Renderer) renderSection(w io.Writer, n node, stack []any, templatePath string) error {
	value, found := lookup(n.token.name, stack)
	if !found {
		switch r.options.Missing {
		case MissingError:
			return &Error{Line: n.token.line, Err: fmt.Errorf("missing tag %q", n.token.name)}
		case MissingKeep:
			if _, err := io.WriteString(w, n.token.raw); err != nil {
				return err
			}
			if err := r.renderNodes(w, n.children, stack, templatePath); err != nil {
				return err
			}
			_, err := io.WriteString(w, n.closeRaw)
			return err
		}
	}

	if n.token.kind == tokenInverted {
		if !truthy(value) {
			return r.renderNodes(w, n.children, stack, templatePath)
		}
		return nil
	}

	items, isList := asList(value)
	if isList {
		for _, item := range items {
			if err := r.renderNodes(w, n.children, append(stack, item), templatePath); err != nil {
				return err
			}
		}
		return nil
	}
	if truthy(value) {
		return r.renderNodes(w, n.children, append(stack, value), templatePath)
	}
	return nil
}

func (r *Renderer) renderNodes(w io.Writer, nodes []node, stack []any, templatePath string) error {
	for _, n := range nodes {
		switch n.token.kind {
		case tokenText:
			if _, err := io.WriteString(w, n.token.raw); err != nil {
				return err
			}
		case tokenVariable, tokenUnescaped:
			if err := r.renderVariable(w, n.token, stack); err != nil {
				return err
			}
		case tokenSection, tokenInverted:
			if err := r.renderSection(w, n, stack, templatePath); err != nil {
				return err
			}
		case tokenPartial:
			if err := r.renderPartial(w, n.token, stack, templatePath); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) renderPartial(w io.Writer, tok token, stack []any, templatePath string) error {
	name := tok.name
	if strings.HasPrefix(name, "*") {
		if strings.HasPrefix(name, "**") {
			return nil
		}
		value, found := lookup(name[1:], stack)
		if !found {
			switch r.options.Missing {
			case MissingError:
				return &Error{Line: tok.line, Err: fmt.Errorf("missing tag %q", name[1:])}
			case MissingKeep:
				_, err := io.WriteString(w, tok.raw)
				return err
			default:
				return nil
			}
		}
		name = stringify(value)
	}
	partial, ok := r.options.Partials[name]
	partialPath := templatePath
	if !ok && templatePath != "" {
		var err error
		partial, partialPath, ok, err = r.LoadPartial(templatePath, name)
		if err != nil {
			return &Error{Line: tok.line, Err: err}
		}
	}
	if !ok {
		if r.options.Missing == MissingKeep {
			_, err := io.WriteString(w, tok.raw)
			return err
		}
		return nil
	}
	if tok.indent != "" {
		partial = indent(partial, tok.indent)
	}
	l := newLexer(strings.NewReader(partial), r.options.Delimiters[0], r.options.Delimiters[1])
	return r.renderStream(w, l, stack, partialPath)
}

func (r *Renderer) LoadPartial(templatePath, name string) (string, string, bool, error) {
	partialPath := filepath.FromSlash(name)
	if !filepath.IsAbs(partialPath) {
		partialPath = filepath.Join(filepath.Dir(templatePath), partialPath)
	}
	partialPath, err := filepath.Abs(partialPath)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve partial path: %w", err)
	}
	contents, err := os.ReadFile(partialPath)
	if os.IsNotExist(err) {
		if r.options.Missing == MissingError {
			return "", partialPath, false, fmt.Errorf("partial file not found: %s", partialPath)
		}
		return "", partialPath, false, nil
	}
	if err != nil {
		return "", partialPath, false, fmt.Errorf("read partial file %s: %w", partialPath, err)
	}
	return string(contents), partialPath, true, nil
}

func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	indented := prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
	if strings.HasSuffix(s, "\n") {
		indented = strings.TrimSuffix(indented, prefix)
	}
	return indented
}

func lookup(name string, stack []any) (any, bool) {
	if name == "." {
		return stack[len(stack)-1], true
	}
	parts := strings.Split(name, ".")
	var value any
	found := false
	for i := len(stack) - 1; i >= 0; i-- {
		if value, found = mapValue(stack[i], parts[0]); found {
			break
		}
	}
	if !found {
		return nil, false
	}
	for _, part := range parts[1:] {
		value, found = mapValue(value, part)
		if !found {
			return nil, false
		}
	}
	return value, true
}

func mapValue(value any, key string) (any, bool) {
	switch m := value.(type) {
	case map[string]any:
		v, ok := m[key]
		return v, ok
	case map[any]any:
		v, ok := m[key]
		return v, ok
	default:
		return nil, false
	}
}

func asList(value any) ([]any, bool) {
	if value == nil {
		return nil, false
	}
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return nil, false
	}
	items := make([]any, v.Len())
	for i := range items {
		items[i] = v.Index(i).Interface()
	}
	return items, true
}

func truthy(value any) bool {
	if value == nil {
		return false
	}
	if b, ok := value.(bool); ok {
		return b
	}
	v := reflect.ValueOf(value)
	return (v.Kind() != reflect.Slice && v.Kind() != reflect.Array) || v.Len() != 0
}

func stringify(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

var htmlEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
	"\"", "&quot;",
	"'", "&#39;",
)

func escapeHTML(value string) string {
	return htmlEscaper.Replace(value)
}

type tokenKind uint8

const (
	tokenText tokenKind = iota
	tokenVariable
	tokenUnescaped
	tokenSection
	tokenInverted
	tokenEnd
	tokenPartial
	tokenComment
	tokenDelimiter
)

type token struct {
	kind   tokenKind
	name   string
	raw    string
	indent string
	line   int
}

type lexer struct {
	reader *bufio.Reader
	open   string
	close  string
	queue  []token
	eof    bool
	line   int
}

func newLexer(r io.Reader, open, close string) *lexer {
	return &lexer{reader: bufio.NewReader(r), open: open, close: close, line: 1}
}

func (l *lexer) next() (token, error) {
	for len(l.queue) == 0 {
		if l.eof {
			return token{}, io.EOF
		}
		// TODO: reading line by line might lead to loading large files into memory inefficiently.
		// Modify this to read in chunks instead.
		lineNumber := l.line
		line, err := l.reader.ReadString('\n')
		if errors.Is(err, io.EOF) {
			l.eof = true
		} else if err != nil {
			return token{}, err
		}
		l.line += strings.Count(line, "\n")
		if line == "" && l.eof {
			return token{}, io.EOF
		}
		tokens, err := l.tokenize(line, lineNumber)
		if err != nil {
			return token{}, err
		}
		l.queue = tokens
	}
	tok := l.queue[0]
	l.queue = l.queue[1:]
	return tok, nil
}

func (l *lexer) tokenize(input string, line int) ([]token, error) {
	var tokens []token
	pos := 0
	for {
		start := strings.Index(input[pos:], l.open)
		if start < 0 {
			if pos < len(input) {
				tokens = append(tokens, token{kind: tokenText, raw: input[pos:]})
			}
			break
		}
		start += pos
		if start > pos {
			tokens = append(tokens, token{kind: tokenText, raw: input[pos:start]})
		}

		triple := l.open == "{{" && l.close == "}}" && strings.HasPrefix(input[start:], "{{{")
		close := l.close
		contentStart := start + len(l.open)
		if triple {
			close = "}}}"
			contentStart++
		}
		end := strings.Index(input[contentStart:], close)
		for end < 0 && !l.eof {
			more, err := l.reader.ReadString('\n')
			if errors.Is(err, io.EOF) {
				l.eof = true
			} else if err != nil {
				return nil, err
			}
			l.line += strings.Count(more, "\n")
			input += more
			end = strings.Index(input[contentStart:], close)
		}
		if end < 0 {
			return nil, fmt.Errorf("unclosed tag")
		}
		end += contentStart
		raw := input[start : end+len(close)]
		inner := input[contentStart:end]
		tok, err := l.makeToken(raw, inner, triple)
		if err != nil {
			return nil, err
		}
		tok.line = line + strings.Count(input[:start], "\n")
		tokens = append(tokens, tok)
		pos = end + len(close)
	}

	return standalone(tokens), nil
}

func (l *lexer) makeToken(raw, inner string, triple bool) (token, error) {
	if triple {
		name := strings.TrimSpace(inner)
		if err := validateName(name); err != nil {
			return token{}, err
		}
		return token{kind: tokenUnescaped, name: name, raw: raw}, nil
	}

	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return token{}, fmt.Errorf("empty tag")
	}
	kind := tokenVariable
	name := trimmed
	switch trimmed[0] {
	case '&':
		kind, name = tokenUnescaped, strings.TrimSpace(trimmed[1:])
	case '#':
		kind, name = tokenSection, strings.TrimSpace(trimmed[1:])
	case '^':
		kind, name = tokenInverted, strings.TrimSpace(trimmed[1:])
	case '/':
		kind, name = tokenEnd, strings.TrimSpace(trimmed[1:])
	case '>':
		kind, name = tokenPartial, strings.TrimSpace(trimmed[1:])
		if strings.HasPrefix(name, "*") {
			name = "*" + strings.TrimSpace(name[1:])
		}
	case '!':
		return token{kind: tokenComment, raw: raw}, nil
	case '=':
		if !strings.HasSuffix(trimmed, "=") {
			return token{}, fmt.Errorf("invalid set delimiter tag")
		}
		fields := strings.Fields(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
		if len(fields) != 2 {
			return token{}, fmt.Errorf("set delimiter tag requires two delimiters")
		}
		if err := validateDelimiters(fields[0], fields[1]); err != nil {
			return token{}, err
		}
		l.open, l.close = fields[0], fields[1]
		return token{kind: tokenDelimiter, raw: raw}, nil
	}
	if err := validateName(name); err != nil {
		return token{}, err
	}
	return token{kind: kind, name: name, raw: raw}, nil
}

func standalone(tokens []token) []token {
	eligible := 0
	var text strings.Builder
	for _, tok := range tokens {
		if tok.kind == tokenText {
			text.WriteString(tok.raw)
			continue
		}
		switch tok.kind {
		case tokenSection, tokenInverted, tokenEnd, tokenPartial, tokenComment, tokenDelimiter:
			eligible++
		default:
			return tokens
		}
	}
	if eligible != 1 {
		return tokens
	}
	whitespace := strings.TrimSuffix(text.String(), "\n")
	whitespace = strings.TrimSuffix(whitespace, "\r")
	if strings.Trim(whitespace, " \t") != "" {
		return tokens
	}

	result := tokens[:0]
	for i, tok := range tokens {
		if tok.kind == tokenText {
			continue
		}
		for _, adjacent := range tokens[:i] {
			if adjacent.kind == tokenText {
				tok.raw = adjacent.raw + tok.raw
			}
		}
		for _, adjacent := range tokens[i+1:] {
			if adjacent.kind == tokenText {
				tok.raw += adjacent.raw
			}
		}
		if tok.kind == tokenPartial {
			tok.indent = whitespace
		}
		result = append(result, tok)
	}
	return result
}

func validateName(name string) error {
	if name == "" || len(strings.Fields(name)) != 1 {
		return fmt.Errorf("invalid tag name %q", name)
	}
	return nil
}

func validateDelimiters(open, close string) error {
	if open == "" || close == "" || strings.ContainsAny(open, " \t\r\n") || strings.ContainsAny(close, " \t\r\n") {
		return fmt.Errorf("delimiters must be two non-whitespace strings")
	}
	return nil
}
