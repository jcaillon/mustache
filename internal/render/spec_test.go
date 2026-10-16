package render

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type specFile struct {
	Tests []specTest `yaml:"tests"`
}

type specTest struct {
	Name     string            `yaml:"name"`
	Data     any               `yaml:"data"`
	Template string            `yaml:"template"`
	Expected string            `yaml:"expected"`
	Partials map[string]string `yaml:"partials"`
}

func TestDecodeSingleDocumentAcceptsEmptyDocument(t *testing.T) {
	for _, input := range []string{"", " \n", "---\n"} {
		document, err := decodeSingleDocument(strings.NewReader(input), "data.yml")
		values, ok := document.(map[any]any)
		if err != nil || !ok || len(values) != 0 {
			t.Fatalf("input=%q document=%#v err=%v", input, document, err)
		}
	}
}

func TestActiveSpecs(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "specs", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if strings.HasPrefix(filepath.Base(file), "~") {
			continue
		}
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var root yaml.Node
		if err := yaml.Unmarshal(contents, &root); err != nil {
			t.Fatal(err)
		}
		var spec specFile
		if err := root.Decode(&spec); err != nil {
			t.Fatal(err)
		}
		for i := range spec.Tests {
			dataNode := root.Content[0]
			for j := 0; j < len(dataNode.Content); j += 2 {
				if dataNode.Content[j].Value != "tests" {
					continue
				}
				testNode := dataNode.Content[j+1].Content[i]
				for k := 0; k < len(testNode.Content); k += 2 {
					if testNode.Content[k].Value == "data" {
						data, err := DecodeData(testNode.Content[k+1])
						if err != nil {
							t.Fatal(err)
						}
						spec.Tests[i].Data = data
					}
				}
			}
		}
		for _, test := range spec.Tests {
			test := test
			t.Run(filepath.Base(file)+"/"+test.Name, func(t *testing.T) {
				renderer, err := New(Options{Missing: MissingEmpty, Partials: test.Partials})
				if err != nil {
					t.Fatal(err)
				}
				var output strings.Builder
				if err := renderer.Render(&output, strings.NewReader(test.Template), "", test.Data); err != nil {
					t.Fatal(err)
				}
				if output.String() != test.Expected {
					t.Errorf("rendered output mismatch\nwant: %q\n got: %q", test.Expected, output.String())
				}
			})
		}
	}
}

func TestRenderStreamsTopLevelTemplate(t *testing.T) {
	for _, template := range []string{
		strings.Repeat("x\n", 100_001),
		strings.Repeat("x", 1_000_001),
	} {
		reader := &limitedTemplateReader{
			prefix: template,
			err:    fmt.Errorf("template read continued after output failed"),
		}
		renderer, err := New(Options{Missing: MissingEmpty})
		if err != nil {
			t.Fatal(err)
		}
		err = renderer.Render(&failAfterWriter{remaining: 2}, reader, "", map[string]any{})
		if !strings.Contains(fmt.Sprint(err), "output stopped") {
			t.Fatalf("expected output error before reader error, got %v", err)
		}
		if reader.read >= len(reader.prefix) {
			t.Fatalf("renderer read the entire %d-byte template before producing output", len(template))
		}
	}
}

func TestRenderAcrossBufferBoundaries(t *testing.T) {
	tests := []struct {
		name     string
		template string
		want     string
	}{
		{name: "delimiter", template: strings.Repeat("x", 4095) + "{{name}}", want: strings.Repeat("x", 4095) + "Ada"},
		{name: "standalone", template: strings.Repeat(" ", 5000) + "{{! comment}}\nafter", want: "after"},
		{name: "indented", template: strings.Repeat("x", 5000) + "{{|text}}", want: strings.Repeat("x", 5000) + "Ada\n" + strings.Repeat(" ", 5000) + "Lovelace"},
		{name: "indented unicode", template: strings.Repeat("x", 4095) + "é{{|text}}", want: strings.Repeat("x", 4095) + "éAda\n" + strings.Repeat(" ", 4096) + "Lovelace"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			renderer, err := New(Options{Missing: MissingEmpty})
			if err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			if err := renderer.Render(&output, strings.NewReader(test.template), "", map[string]any{"name": "Ada", "text": "Ada\nLovelace"}); err != nil {
				t.Fatal(err)
			}
			if output.String() != test.want {
				t.Fatalf("rendered output mismatch: want %q, got %q", test.want, output.String())
			}
		})
	}
}

func TestRenderPartialUsesConfiguredDelimiters(t *testing.T) {
	renderer, err := New(Options{
		Missing:    MissingEmpty,
		Partials:   map[string]string{"user": "<%name%>"},
		Delimiters: [2]string{"<%", "%>"},
	})
	if err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	if err := renderer.Render(&output, strings.NewReader("<%>user%>"), "", map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Ada" {
		t.Fatalf("rendered output mismatch: want %q, got %q", "Ada", output.String())
	}
}

func TestDisableHTMLEscape(t *testing.T) {
	renderer, err := New(Options{Missing: MissingEmpty, DisableHTMLEscape: true})
	if err != nil {
		t.Fatal(err)
	}

	var output strings.Builder
	if err := renderer.Render(&output, strings.NewReader("{{value}}"), "", map[string]any{"value": "<b>&</b>"}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "<b>&</b>" {
		t.Fatalf("rendered output mismatch: want %q, got %q", "<b>&</b>", output.String())
	}
}

func TestIndentedInterpolationOptions(t *testing.T) {
	tests := []struct {
		name     string
		options  Options
		template string
		want     string
	}{
		{
			name:     "disable HTML escape",
			options:  Options{DisableHTMLEscape: true},
			template: "  {{|text}}",
			want:     "  <b>\n  &",
		},
		{
			name:     "keep missing",
			options:  Options{Missing: MissingKeep},
			template: "  {{ |missing }}\n",
			want:     "  {{ |missing }}\n",
		},
		{
			name:     "configured delimiters",
			options:  Options{Delimiters: [2]string{"<%", "%>"}},
			template: "ID: <%|text%>",
			want:     "ID: &lt;b&gt;\n    &amp;",
		},
		{
			name:     "inline block",
			template: "ID: {{$body}}{{|text}}{{/body}}",
			want:     "ID: &lt;b&gt;\n    &amp;",
		},
		{
			name:     "reindented block",
			options:  Options{Partials: map[string]string{"layout": "  {{$body}}{{/body}}"}},
			template: "{{<layout}}{{$body}}\n    {{|text}}\n{{/body}}{{/layout}}",
			want:     "  &lt;b&gt;\n  &amp;\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			renderer, err := New(test.options)
			if err != nil {
				t.Fatal(err)
			}
			var output strings.Builder
			if err := renderer.Render(&output, strings.NewReader(test.template), "", map[string]any{"text": "<b>\n&"}); err != nil {
				t.Fatal(err)
			}
			if output.String() != test.want {
				t.Fatalf("rendered output mismatch: want %q, got %q", test.want, output.String())
			}
		})
	}

	renderer, err := New(Options{Missing: MissingError})
	if err != nil {
		t.Fatal(err)
	}
	err = renderer.Render(&strings.Builder{}, strings.NewReader("\n  {{|missing}}"), "", map[string]any{})
	var renderErr *Error
	if !errors.As(err, &renderErr) || renderErr.Line != 2 || renderErr.Err.Error() != `missing tag "missing"` {
		t.Fatalf("expected contextual missing-tag error, got %v", err)
	}
	err = renderer.Render(&failAfterWriter{remaining: 2}, strings.NewReader("  {{|text}}"), "", map[string]any{"text": "first\nsecond"})
	if !strings.Contains(fmt.Sprint(err), "output stopped") {
		t.Fatalf("expected output error, got %v", err)
	}
}

func TestVerboseLookupUsesPartialPathAndLine(t *testing.T) {
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.mustache")
	partialPath := filepath.Join(dir, "partial.mustache")
	if err := os.WriteFile(partialPath, []byte("first\n{{name}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var verbose strings.Builder
	renderer, err := New(Options{Missing: MissingEmpty, Verbose: &verbose})
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := renderer.Render(&output, strings.NewReader("{{>partial.mustache}}"), templatePath, map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "first\nAda\n" {
		t.Fatalf("rendered output mismatch: %q", output.String())
	}
	want := fmt.Sprintf("mustache: verbose: lookup %q in %s:2: found", "name", partialPath)
	if !strings.Contains(verbose.String(), want) {
		t.Fatalf("verbose output does not contain %q:\n%s", want, verbose.String())
	}
}

func TestLoadPartialAppendsMustacheSuffix(t *testing.T) {
	dir := t.TempDir()
	templatePath := filepath.Join(dir, "template.mustache")
	partialPath := filepath.Join(dir, "article.mustache")
	if err := os.WriteFile(partialPath, []byte("Hello, {{name}}!"), 0o600); err != nil {
		t.Fatal(err)
	}

	renderer, err := New(Options{Missing: MissingError})
	if err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	if err := renderer.Render(&output, strings.NewReader("{{>article}}"), templatePath, map[string]any{"name": "Ada"}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Hello, Ada!" {
		t.Fatalf("rendered output mismatch: want %q, got %q", "Hello, Ada!", output.String())
	}
}

func TestKeepMissingStandalonePartials(t *testing.T) {
	renderer, err := New(Options{Missing: MissingKeep})
	if err != nil {
		t.Fatal(err)
	}

	template := "before\n  {{>missing}}\n{{>*missingName}}\nafter"
	var output strings.Builder
	if err := renderer.Render(&output, strings.NewReader(template), "", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if output.String() != template {
		t.Fatalf("rendered output mismatch: want %q, got %q", template, output.String())
	}
}

func TestMalformedSections(t *testing.T) {
	tests := []string{
		"{{#a}}",
		"{{#a}}{{/b}}",
		"{{/a}}",
		"{{name",
		"{{|}}",
		"{{|two names}}",
	}
	for _, template := range tests {
		t.Run(template, func(t *testing.T) {
			renderer, err := New(Options{Missing: MissingEmpty})
			if err != nil {
				t.Fatal(err)
			}
			if err := renderer.Render(&bytes.Buffer{}, strings.NewReader(template), "", map[string]any{}); err == nil {
				t.Fatal("expected malformed template error")
			}
		})
	}
}

func TestBashLambdaReceivesRawSectionWithBraces(t *testing.T) {
	renderer, err := New(Options{Missing: MissingEmpty})
	if err != nil {
		t.Fatal(err)
	}

	data := map[string]any{
		"x":      "Error!",
		"lambda": BashLambda(`if [[ $1 == "{{x}}" ]]; then printf yes; else printf %s "$1"; fi`),
	}
	var output strings.Builder
	if err := renderer.Render(&output, strings.NewReader("<{{#lambda}}{{x}}{{/lambda}}>"), "", data); err != nil {
		t.Fatal(err)
	}
	if output.String() != "<yes>" {
		t.Fatalf("rendered output mismatch: want %q, got %q", "<yes>", output.String())
	}
}

func TestBashSectionLambdaReportsCommandFailure(t *testing.T) {
	renderer, err := New(Options{Missing: MissingEmpty})
	if err != nil {
		t.Fatal(err)
	}

	err = renderer.Render(&strings.Builder{}, strings.NewReader("{{#lambda}}text{{/lambda}}"), "", map[string]any{
		"lambda": BashLambda("exit 3"),
	})
	var renderErr *Error
	if !errors.As(err, &renderErr) || renderErr.Line != 1 || !strings.Contains(renderErr.Err.Error(), "execute bash lambda") {
		t.Fatalf("expected contextual lambda error, got %v", err)
	}
}

type limitedTemplateReader struct {
	prefix string
	read   int
	err    error
}

func (r *limitedTemplateReader) Read(p []byte) (int, error) {
	if r.read == len(r.prefix) {
		return 0, r.err
	}
	n := copy(p, r.prefix[r.read:])
	r.read += n
	return n, nil
}

type failAfterWriter struct {
	remaining int
}

func (w *failAfterWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		return 0, fmt.Errorf("output stopped")
	}
	w.remaining -= len(p)
	return len(p), nil
}
