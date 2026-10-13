package render

import (
	"bytes"
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
	const lines = 100_001
	reader := &limitedTemplateReader{
		prefix: strings.Repeat("x\n", lines),
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
		t.Fatalf("renderer read the entire %d-line template before producing output", lines)
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
