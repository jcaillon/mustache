package render

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
)

type MissingBehavior uint8

const (
	MissingError MissingBehavior = iota
	MissingEmpty
	MissingKeep
)

type Options struct {
	Missing           MissingBehavior
	Partials          map[string]string
	Delimiters        [2]string
	Verbose           io.Writer
	DisableHTMLEscape bool
	DisableLambda     bool
}

type Renderer struct {
	options Options
}

type Error struct {
	File string
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
	r.logf("rendering template %s", displayTemplatePath(templatePath))
	l := newLexer(template, r.options.Delimiters[0], r.options.Delimiters[1])
	err := r.renderStream(&renderWriter{Writer: w}, l, []any{data}, templatePath, nil)
	if err == nil {
		r.logf("finished template %s", displayTemplatePath(templatePath))
	}
	return err
}

type renderWriter struct {
	io.Writer
	line []byte
}

func (w *renderWriter) Write(p []byte) (int, error) {
	n, err := w.Writer.Write(p)
	w.track(string(p[:n]))
	return n, err
}

func (w *renderWriter) WriteString(s string) (int, error) {
	n, err := io.WriteString(w.Writer, s)
	w.track(s[:n])
	return n, err
}

func (w *renderWriter) track(s string) {
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		w.line = w.line[:0]
		s = s[i+1:]
	}
	w.line = append(w.line, s...)
}

func (r *Renderer) renderStream(w *renderWriter, l *lexer, stack []any, templatePath string, overrides map[string]block) error {
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
			if err := r.renderVariable(w, tok, stack, templatePath); err != nil {
				return err
			}
		case tokenSection, tokenInverted, tokenParent, tokenBlock, tokenIf:
			node, err := parseSection(l, tok)
			if err != nil {
				return err
			}
			if err := r.renderNode(w, node, stack, templatePath, overrides); err != nil {
				return err
			}
		case tokenEnd:
			return fmt.Errorf("unexpected closing section %q", tok.name)
		case tokenElif, tokenElse, tokenFi:
			return fmt.Errorf("unexpected conditional tag %q", tok.raw)
		case tokenPartial:
			if err := r.renderPartial(w, tok, stack, templatePath, overrides); err != nil {
				return err
			}
		}
	}
}

func (r *Renderer) renderVariable(w *renderWriter, tok token, stack []any, templatePath string) error {
	text, found, err := r.variableValue(tok, stack, templatePath)
	if err != nil {
		return err
	}
	if !found {
		if r.options.Missing == MissingKeep {
			_, err := io.WriteString(w, tok.raw)
			return err
		}
		return nil
	}
	if tok.kind == tokenVariable && !r.options.DisableHTMLEscape {
		text = escapeHTML(text)
	}
	if tok.indented && strings.Contains(text, "\n") {
		prefix := strings.Map(func(c rune) rune {
			if c == '\t' {
				return c
			}
			return ' '
		}, string(w.line))
		text = strings.TrimPrefix(indent(text, prefix), prefix)
	}
	_, err = io.WriteString(w, text)
	return err
}

func (r *Renderer) variableValue(tok token, stack []any, templatePath string) (string, bool, error) {
	value, found := lookup(tok.name, stack)
	r.logLookup(tok.name, templatePath, tok.line, found)
	if !found {
		switch r.options.Missing {
		case MissingEmpty, MissingKeep:
			return "", false, nil
		default:
			return "", false, &Error{File: templatePath, Line: tok.line, Err: fmt.Errorf("missing tag %q", tok.name)}
		}
	}

	text := stringify(value)
	if lambda, ok := value.(BashLambda); ok {
		output, err := r.runBashLambda(lambda)
		if err != nil {
			return "", true, &Error{File: templatePath, Line: tok.line, Err: fmt.Errorf("execute bash lambda: %w", err)}
		}
		var rendered strings.Builder
		if err := r.renderStream(&renderWriter{Writer: &rendered}, newLexer(bytes.NewReader(output), "{{", "}}"), stack, "(lambda code for tag "+tok.name+")", nil); err != nil {
			return "", true, err
		}
		text = rendered.String()
	}
	return text, true, nil
}

type node struct {
	token    token
	children []node
	closeRaw string
}

func parseSection(l *lexer, opener token) (node, error) {
	n := node{token: opener}
	closingKind := tokenEnd
	if opener.kind == tokenIf {
		closingKind = tokenFi
	}
	seenElse := false
	for {
		tok, err := l.next()
		if errors.Is(err, io.EOF) {
			if opener.kind == tokenIf {
				return node{}, fmt.Errorf("unclosed conditional")
			}
			return node{}, fmt.Errorf("unclosed section %q", opener.name)
		}
		if err != nil {
			return node{}, err
		}
		if tok.kind == tokenEnd || tok.kind == tokenFi {
			if tok.kind != closingKind || tok.kind == tokenEnd && tok.name != opener.name {
				return node{}, fmt.Errorf("section %q closed by %q", opener.name, tok.name)
			}
			n.closeRaw = tok.raw
			if opener.kind == tokenBlock && strings.HasSuffix(opener.raw, "\n") && !strings.HasSuffix(tok.raw, "\n") {
				next, err := l.next()
				if err == nil && next.kind == tokenText && (strings.HasPrefix(next.raw, "\n") || strings.HasPrefix(next.raw, "\r\n")) {
					newline := "\n"
					if strings.HasPrefix(next.raw, "\r\n") {
						newline = "\r\n"
					}
					n.children = append(n.children, node{token: token{kind: tokenText, raw: newline}})
					next.raw = strings.TrimPrefix(next.raw, newline)
					if next.raw != "" {
						l.queue = append([]token{next}, l.queue...)
					}
				} else if err == nil {
					l.queue = append([]token{next}, l.queue...)
				} else if !errors.Is(err, io.EOF) {
					return node{}, err
				}
			}
			return n, nil
		}
		if tok.kind == tokenElif || tok.kind == tokenElse {
			if opener.kind != tokenIf || seenElse {
				return node{}, fmt.Errorf("unexpected conditional branch %q", tok.raw)
			}
			seenElse = tok.kind == tokenElse
		}
		if tok.kind == tokenSection || tok.kind == tokenInverted || tok.kind == tokenParent || tok.kind == tokenBlock || tok.kind == tokenIf {
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

type block struct {
	children []node
	indent   string
}

func (r *Renderer) renderNode(w *renderWriter, n node, stack []any, templatePath string, overrides map[string]block) error {
	switch n.token.kind {
	case tokenParent:
		return r.renderParent(w, n, stack, templatePath, overrides)
	case tokenBlock:
		return r.renderBlock(w, n, stack, templatePath, overrides)
	case tokenIf:
		return r.renderConditional(w, n, stack, templatePath, overrides)
	default:
		return r.renderSection(w, n, stack, templatePath, overrides)
	}
}

func (r *Renderer) renderConditional(w *renderWriter, n node, stack []any, templatePath string, overrides map[string]block) error {
	branch := n.token
	start := 0
	for i := 0; i <= len(n.children); i++ {
		if i < len(n.children) && n.children[i].token.kind != tokenElif && n.children[i].token.kind != tokenElse {
			continue
		}
		if branch.kind == tokenElse {
			return r.renderNodes(w, n.children[start:i], stack, templatePath, overrides)
		}
		text, found, err := r.variableValue(branch, stack, templatePath)
		if err != nil {
			return err
		}
		if !found && r.options.Missing == MissingKeep {
			_, err := io.WriteString(w, rawNodes([]node{n}))
			return err
		}
		matched := text == branch.operand
		if branch.pattern != nil {
			matched = branch.pattern.MatchString(text)
		}
		if branch.operator == "!=" || branch.operator == "!~" {
			matched = !matched
		}
		if matched {
			return r.renderNodes(w, n.children[start:i], stack, templatePath, overrides)
		}
		if i < len(n.children) {
			branch = n.children[i].token
			start = i + 1
		}
	}
	return nil
}

func (r *Renderer) renderSection(w *renderWriter, n node, stack []any, templatePath string, overrides map[string]block) error {
	value, found := lookup(n.token.name, stack)
	r.logLookup(n.token.name, templatePath, n.token.line, found)
	if !found {
		switch r.options.Missing {
		case MissingError:
			return &Error{File: templatePath, Line: n.token.line, Err: fmt.Errorf("missing tag %q", n.token.name)}
		case MissingKeep:
			if _, err := io.WriteString(w, n.token.raw); err != nil {
				return err
			}
			if err := r.renderNodes(w, n.children, stack, templatePath, overrides); err != nil {
				return err
			}
			_, err := io.WriteString(w, n.closeRaw)
			return err
		}
	}

	if lambda, ok := value.(BashLambda); ok {
		output, err := r.runBashLambda(lambda, rawNodes(n.children))
		if err != nil {
			if n.token.kind == tokenInverted && !r.options.DisableLambda {
				return nil
			}
			return &Error{File: templatePath, Line: n.token.line, Err: fmt.Errorf("execute bash lambda: %w", err)}
		}
		return r.renderStream(w, newLexer(bytes.NewReader(output), n.token.open, n.token.close), stack, templatePath, overrides)
	}

	if n.token.kind == tokenInverted {
		if !truthy(value) {
			return r.renderNodes(w, n.children, stack, templatePath, overrides)
		}
		return nil
	}

	if items, ok := listValue(value); ok {
		for i := 0; i < items.Len(); i++ {
			if err := r.renderNodes(w, n.children, append(stack, items.Index(i).Interface()), templatePath, overrides); err != nil {
				return err
			}
		}
		return nil
	}
	if truthy(value) {
		return r.renderNodes(w, n.children, append(stack, value), templatePath, overrides)
	}
	return nil
}

func (r *Renderer) runBashLambda(lambda BashLambda, args ...string) ([]byte, error) {
	if r.options.DisableLambda {
		return nil, errors.New("lambda usage is disabled (--disable-lambda)")
	}
	commandArgs := []string{"-c", string(lambda)}
	hasArg := len(args) != 0
	if hasArg {
		if runtime.GOOS == "windows" {
			// MSYS2 can alter argv, so pass the raw section through the environment.
			commandArgs[1] = `set -- "$MUSTACHE_LAMBDA_ARG"; ` + commandArgs[1]
		}
		commandArgs = append(commandArgs, "-s", args[0])
	}
	cmd := exec.Command("bash", commandArgs...)
	cmd.Env = os.Environ()
	if runtime.GOOS == "windows" {
		if hasArg {
			cmd.Env = append(cmd.Env, "MUSTACHE_LAMBDA_ARG="+args[0])
		}
		cmd.Env = append(cmd.Env, "MSYS2_ARG_CONV_EXCL=*")
	}
	r.logf("executing bash command args=%q", cmd.Args)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	r.logf("bash command stdout=%q stderr=%q exit_code=%d", stdout.String(), stderr.String(), exitCode)
	return stdout.Bytes(), err
}

func rawNodes(nodes []node) string {
	var raw strings.Builder
	writeRawNodes(&raw, nodes)
	return raw.String()
}

func writeRawNodes(raw *strings.Builder, nodes []node) {
	for _, n := range nodes {
		raw.WriteString(n.token.raw)
		if n.token.kind == tokenSection || n.token.kind == tokenInverted || n.token.kind == tokenParent || n.token.kind == tokenBlock || n.token.kind == tokenIf {
			writeRawNodes(raw, n.children)
			raw.WriteString(n.closeRaw)
		}
	}
}

func (r *Renderer) renderNodes(w *renderWriter, nodes []node, stack []any, templatePath string, overrides map[string]block) error {
	for _, n := range nodes {
		switch n.token.kind {
		case tokenText:
			if _, err := io.WriteString(w, n.token.raw); err != nil {
				return err
			}
		case tokenVariable, tokenUnescaped:
			if err := r.renderVariable(w, n.token, stack, templatePath); err != nil {
				return err
			}
		case tokenSection, tokenInverted, tokenParent, tokenBlock, tokenIf:
			if err := r.renderNode(w, n, stack, templatePath, overrides); err != nil {
				return err
			}
		case tokenPartial:
			if err := r.renderPartial(w, n.token, stack, templatePath, overrides); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Renderer) renderPartial(w *renderWriter, tok token, stack []any, templatePath string, overrides map[string]block) error {
	name := tok.name
	if strings.HasPrefix(name, "*") {
		if strings.HasPrefix(name, "**") {
			return nil
		}
		value, found := lookup(name[1:], stack)
		r.logLookup(name[1:], templatePath, tok.line, found)
		if !found {
			switch r.options.Missing {
			case MissingError:
				return &Error{File: templatePath, Line: tok.line, Err: fmt.Errorf("missing tag %q", name[1:])}
			case MissingKeep:
				_, err := io.WriteString(w, tok.raw)
				return err
			default:
				return nil
			}
		}
		name = stringify(value)
	}
	partial, partialPath, ok, err := r.loadPartial(templatePath, name)
	if err != nil {
		return &Error{File: templatePath, Line: tok.line, Err: err}
	}
	if !ok {
		if r.options.Missing == MissingKeep {
			_, err := io.WriteString(w, tok.raw)
			return err
		}
		return nil
	}
	defer partial.Close()
	r.logf("rendering partial %q from %s", name, displayTemplatePath(partialPath))
	var source io.Reader = partial
	if tok.indent != "" {
		source = newIndentReader(partial, tok.indent)
	}
	l := newLexer(source, r.options.Delimiters[0], r.options.Delimiters[1])
	return r.renderStream(w, l, stack, partialPath, overrides)
}

func (r *Renderer) renderParent(w *renderWriter, n node, stack []any, templatePath string, overrides map[string]block) error {
	partial, partialPath, ok, err := r.loadPartial(templatePath, n.token.name)
	if err != nil {
		return &Error{File: templatePath, Line: n.token.line, Err: err}
	}
	if !ok {
		if r.options.Missing == MissingKeep {
			_, err = io.WriteString(w, n.token.raw+rawNodes(n.children)+n.closeRaw)
			return err
		}
		return nil
	}
	defer partial.Close()

	merged := make(map[string]block, len(overrides))
	for name, value := range overrides {
		merged[name] = value
	}
	for _, child := range n.children {
		if child.token.kind == tokenBlock {
			if _, exists := merged[child.token.name]; !exists {
				merged[child.token.name] = makeBlock(child)
			}
		}
	}
	var source io.Reader = partial
	if n.token.indent != "" {
		source = newIndentReader(partial, n.token.indent)
	}
	r.logf("rendering parent %q from %s", n.token.name, displayTemplatePath(partialPath))
	return r.renderStream(w, newLexer(source, r.options.Delimiters[0], r.options.Delimiters[1]), stack, partialPath, merged)
}

func (r *Renderer) renderBlock(w *renderWriter, n node, stack []any, templatePath string, overrides map[string]block) error {
	defaultBlock := makeBlock(n)
	selected, ok := overrides[n.token.name]
	if !ok {
		selected = defaultBlock
	}
	var output strings.Builder
	if err := r.renderNodes(&renderWriter{Writer: &output, line: append([]byte(nil), w.line...)}, selected.children, stack, templatePath, overrides); err != nil {
		return err
	}
	text := dedent(output.String(), selected.indent)
	if strings.HasSuffix(rawNodes(defaultBlock.children), "\n") && !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	intrinsic := strings.TrimPrefix(defaultBlock.indent, n.token.indent)
	_, err := io.WriteString(w, indent(text, n.token.indent+intrinsic))
	return err
}

func makeBlock(n node) block {
	children := append([]node(nil), n.children...)
	if len(children) != 0 && children[0].token.kind == tokenText {
		raw := children[0].token.raw
		if newline := strings.IndexByte(raw, '\n'); newline >= 0 && strings.Trim(raw[:newline], " \t\r") == "" {
			children[0].token.raw = raw[newline+1:]
			if children[0].token.raw == "" {
				children = children[1:]
			}
		}
	}
	return block{children: children, indent: commonIndent(rawNodes(children))}
}

func commonIndent(s string) string {
	var common string
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		prefix := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
		if common == "" {
			common = prefix
			continue
		}
		for !strings.HasPrefix(prefix, common) {
			common = common[:len(common)-1]
		}
	}
	return common
}

func dedent(s, prefix string) string {
	if prefix == "" {
		return s
	}
	lines := strings.SplitAfter(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimPrefix(lines[i], prefix)
	}
	return strings.Join(lines, "")
}

func (r *Renderer) loadPartial(templatePath, name string) (io.ReadCloser, string, bool, error) {
	partial, ok := r.options.Partials[name]
	if ok || templatePath == "" {
		if !ok {
			return nil, templatePath, false, nil
		}
		return io.NopCloser(strings.NewReader(partial)), templatePath, true, nil
	}
	return r.LoadPartial(templatePath, name)
}

func (r *Renderer) logLookup(name, templatePath string, line int, found bool) {
	if r.options.Verbose != nil {
		result := "missing"
		if found {
			result = "found"
		}
		r.logf("lookup %q in %s:%d: %s", name, displayTemplatePath(templatePath), line, result)
	}
}

func (r *Renderer) logf(format string, args ...any) {
	if r.options.Verbose != nil {
		fmt.Fprintf(r.options.Verbose, "mustache: verbose: "+format+"\n", args...)
	}
}

func displayTemplatePath(templatePath string) string {
	if templatePath == "" {
		return "<template>"
	}
	return templatePath
}

func (r *Renderer) LoadPartial(templatePath, name string) (io.ReadCloser, string, bool, error) {
	partialPath := filepath.FromSlash(name)
	if !filepath.IsAbs(partialPath) {
		partialPath = filepath.Join(filepath.Dir(templatePath), partialPath)
	}
	partialPath, err := filepath.Abs(partialPath)
	if err != nil {
		return nil, "", false, fmt.Errorf("resolve partial path: %w", err)
	}
	partial, err := os.Open(partialPath)
	if os.IsNotExist(err) && !strings.HasSuffix(partialPath, ".mustache") {
		partialPath += ".mustache"
		partial, err = os.Open(partialPath)
	}
	if os.IsNotExist(err) {
		if r.options.Missing == MissingError {
			return nil, partialPath, false, fmt.Errorf("partial file not found: %s", partialPath)
		}
		return nil, partialPath, false, nil
	}
	if err != nil {
		return nil, partialPath, false, fmt.Errorf("open partial file %s: %w", partialPath, err)
	}
	return partial, partialPath, true, nil
}

type indentReader struct {
	reader      *bufio.Reader
	prefix      string
	pending     []byte
	pendingErr  error
	atLineStart bool
}

func newIndentReader(reader io.Reader, prefix string) io.Reader {
	return &indentReader{reader: bufio.NewReader(reader), prefix: prefix, atLineStart: true}
}

func (r *indentReader) Read(p []byte) (int, error) {
	if len(r.pending) == 0 {
		if r.pendingErr != nil {
			return 0, r.pendingErr
		}
		fragment, err := r.reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			err = nil
		}
		if len(fragment) == 0 {
			return 0, err
		}
		if r.atLineStart {
			r.pending = append([]byte(r.prefix), fragment...)
		} else {
			r.pending = append(r.pending, fragment...)
		}
		r.atLineStart = fragment[len(fragment)-1] == '\n'
		r.pendingErr = err
	}
	n := copy(p, r.pending)
	r.pending = r.pending[n:]
	return n, nil
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
	key, rest, nested := strings.Cut(name, ".")
	var value any
	found := false
	for i := len(stack) - 1; i >= 0; i-- {
		if value, found = mapValue(stack[i], key); found {
			break
		}
	}
	if !found {
		return nil, false
	}
	for nested {
		key, rest, nested = strings.Cut(rest, ".")
		value, found = mapValue(value, key)
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

func listValue(value any) (reflect.Value, bool) {
	if value == nil {
		return reflect.Value{}, false
	}
	v := reflect.ValueOf(value)
	if v.Kind() != reflect.Slice && v.Kind() != reflect.Array {
		return reflect.Value{}, false
	}
	return v, true
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
	tokenParent
	tokenBlock
	tokenComment
	tokenDelimiter
	tokenIf
	tokenElif
	tokenElse
	tokenFi
)

type token struct {
	kind     tokenKind
	name     string
	raw      string
	indent   string
	indented bool
	line     int
	open     string
	close    string
	operator string
	operand  string
	pattern  *regexp.Regexp
}

type lexer struct {
	reader *bufio.Reader
	open   string
	close  string
	queue  []token
	prefix string
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
		lineNumber := l.line
		fragment, err := l.reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			err = nil
		}
		line := l.prefix + string(fragment)
		l.prefix = ""
		if errors.Is(err, io.EOF) {
			l.eof = true
		} else if err != nil {
			return token{}, err
		}
		l.line += strings.Count(line, "\n")
		if line == "" && l.eof {
			return token{}, io.EOF
		}
		if !strings.Contains(line, l.open) && !strings.HasSuffix(line, "\n") && !l.eof {
			if strings.Trim(line, " \t\r") == "" {
				l.prefix = line
				continue
			}
			keep := delimiterPrefixLen(line, l.open)
			l.prefix = line[len(line)-keep:]
			line = line[:len(line)-keep]
			if line != "" {
				l.queue = []token{{kind: tokenText, raw: line}}
			}
			continue
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

func delimiterPrefixLen(s, delimiter string) int {
	limit := min(len(s), len(delimiter)-1)
	for n := limit; n > 0; n-- {
		if strings.HasSuffix(s, delimiter[:n]) {
			return n
		}
	}
	return 0
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
	open, close := l.open, l.close
	if triple {
		name := strings.TrimSpace(inner)
		if err := validateName(name); err != nil {
			return token{}, err
		}
		return token{kind: tokenUnescaped, name: name, raw: raw, open: open, close: close}, nil
	}

	trimmed := strings.TrimSpace(inner)
	if trimmed == "" {
		return token{}, fmt.Errorf("empty tag")
	}
	kind := tokenVariable
	name := trimmed
	indented := false
	switch trimmed[0] {
	case '|':
		name = strings.TrimSpace(trimmed[1:])
		indented = true
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
	case '<':
		kind, name = tokenParent, strings.TrimSpace(trimmed[1:])
	case '$':
		kind, name = tokenBlock, strings.TrimSpace(trimmed[1:])
	case '!':
		return token{kind: tokenComment, raw: raw, open: open, close: close}, nil
	case '?':
		keyword := strings.Fields(trimmed)[0]
		expression := strings.TrimSpace(trimmed[len(keyword):])
		tok := token{raw: raw, open: open, close: close, name: keyword[1:]}
		switch keyword {
		case "?else", "?fi":
			if expression != "" {
				return token{}, fmt.Errorf("invalid conditional tag %q", trimmed)
			}
			tok.kind = tokenElse
			if keyword == "?fi" {
				tok.kind = tokenFi
			}
			return tok, nil
		case "?if", "?elif":
			tok.kind = tokenIf
			if keyword == "?elif" {
				tok.kind = tokenElif
			}
		default:
			return token{}, fmt.Errorf("unknown conditional tag %q", keyword)
		}
		i := strings.IndexAny(expression, "=!<>")
		if i < 0 || i+2 > len(expression) {
			return token{}, fmt.Errorf("invalid conditional expression %q", expression)
		}
		tok.name = strings.TrimSpace(expression[:i])
		if err := validateName(tok.name); err != nil {
			return token{}, err
		}
		tok.operator = expression[i : i+2]
		tok.operand = strings.TrimSpace(expression[i+2:])
		switch tok.operator {
		case "==", "!=":
		case "=~", "!~":
			pattern, err := regexp.Compile(tok.operand)
			if err != nil {
				return token{}, err
			}
			tok.pattern = pattern
		default:
			return token{}, fmt.Errorf("invalid conditional operator %q", tok.operator)
		}
		return tok, nil
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
		return token{kind: tokenDelimiter, raw: raw, open: open, close: close}, nil
	}
	if err := validateName(name); err != nil {
		return token{}, err
	}
	return token{kind: kind, name: name, raw: raw, indented: indented, open: open, close: close}, nil
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
		case tokenSection, tokenInverted, tokenEnd, tokenPartial, tokenParent, tokenBlock, tokenComment, tokenDelimiter, tokenIf, tokenElif, tokenElse, tokenFi:
			eligible++
		default:
			return tokens
		}
	}
	paired := eligible == 2 && len(tokens) >= 2
	structured := eligible > 1
	direction := tokenKind(0)
	if paired {
		var tags []token
		for _, tok := range tokens {
			if tok.kind != tokenText {
				tags = append(tags, tok)
			}
		}
		paired = (tags[0].kind == tokenParent || tags[0].kind == tokenBlock) && tags[1].kind == tokenEnd && tags[0].name == tags[1].name
	}
	for _, tok := range tokens {
		if tok.kind == tokenText {
			continue
		}
		kind := tok.kind
		if kind == tokenParent || kind == tokenBlock {
			kind = tokenBlock
		}
		if kind != tokenBlock && kind != tokenEnd || direction != 0 && direction != kind {
			structured = false
			break
		}
		direction = kind
	}
	if eligible != 1 && !paired && !structured {
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
		if tok.kind == tokenPartial || tok.kind == tokenParent || tok.kind == tokenBlock {
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
