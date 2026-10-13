package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunMultipleDocuments(t *testing.T) {
	dir := t.TempDir()
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(template, []byte("Hi {{name}}!\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := run([]string{"-", template}, strings.NewReader("---\nname: Ada\n---\nname: Go\n---\n"), &stdout, &stderr)
	if exit != 0 || stdout.String() != "Hi Ada!\nHi Go!\n" || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunEmptyInput(t *testing.T) {
	dir := t.TempDir()
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(template, []byte("rendered\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, input := range []string{"", " \n  "} {
		var stdout, stderr bytes.Buffer
		exit := run([]string{"-", template}, strings.NewReader(input), &stdout, &stderr)
		if exit != 0 || stdout.String() != "rendered\n" || stderr.Len() != 0 {
			t.Fatalf("input=%q exit=%d stdout=%q stderr=%q", input, exit, stdout.String(), stderr.String())
		}
	}
}

func TestRunDisableHTMLEscape(t *testing.T) {
	dir := t.TempDir()
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(template, []byte("{{value}}"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exit := run([]string{"--disable-html-escape", "-", template}, strings.NewReader("value: '<b>text</b>'\n"), &stdout, &stderr)
	if exit != 0 || stdout.String() != "<b>text</b>" || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunMissingTags(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(data, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(template, []byte("value={{missing}} section={{#missing}}yes{{/missing}} inverted={{^missing}}yes{{/missing}}"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		args       []string
		exit       int
		stdout     string
		stderrPart string
	}{
		{
			name:   "missing tags are null by default",
			args:   []string{data, template},
			stdout: "value= section= inverted=yes",
		},
		{
			name:   "keep missing tags",
			args:   []string{"--keep-missing-tags", data, template},
			stdout: "value={{missing}} section={{#missing}}yes{{/missing}} inverted={{^missing}}yes{{/missing}}",
		},
		{
			name:       "strict missing tags",
			args:       []string{"--strict", data, template},
			exit:       1,
			stdout:     "value=",
			stderrPart: "missing tag \"missing\"",
		},
		{
			name:       "conflicting options",
			args:       []string{"--strict", "--keep-missing-tags", data, template},
			exit:       2,
			stderrPart: "cannot be used together",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := run(test.args, strings.NewReader(""), &stdout, &stderr)
			if exit != test.exit || stdout.String() != test.stdout || !strings.Contains(stderr.String(), test.stderrPart) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
		})
	}
}

func TestRunStrictMissingTagLocation(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(data, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(template, []byte("first line\nsecond line\n{{#missingSection}}yes{{/missingSection}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exit := run([]string{"--strict", data, template}, strings.NewReader(""), &stdout, &stderr)
	wantError := fmt.Sprintf("mustache: render template %s:3: missing tag \"missingSection\"\n", template)
	if exit != 1 || stdout.String() != "first line\nsecond line\n" || stderr.String() != wantError {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunPartials(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	partial := filepath.Join(dir, "partial.mustache")
	if err := os.WriteFile(data, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(template, []byte("before\n{{>partial.mustache}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partial, []byte("included"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exit := run([]string{"--strict", data, template}, strings.NewReader(""), &stdout, &stderr)
	if exit != 0 || stdout.String() != "before\nincluded" || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}

	missing := filepath.Join(dir, "missing.mustache")
	if err := os.WriteFile(template, []byte("before\n{{>missing.mustache}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	exit = run([]string{"--strict", data, template}, strings.NewReader(""), &stdout, &stderr)
	wantError := fmt.Sprintf("mustache: render template %s:2: partial file not found: %s\n", template, missing)
	if exit != 1 || stdout.String() != "before\n" || stderr.String() != wantError {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}

	if err := os.WriteFile(template, []byte("before {{> missing.mustache }} after"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	exit = run([]string{"--keep-missing-tags", data, template}, strings.NewReader(""), &stdout, &stderr)
	if exit != 0 || stdout.String() != "before {{> missing.mustache }} after" || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunNestedPartialsRelativeToCurrentTemplate(t *testing.T) {
	dir := t.TempDir()
	partials := filepath.Join(dir, "partials")
	if err := os.Mkdir(partials, 0o700); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	outer := filepath.Join(partials, "outer.mustache")
	inner := filepath.Join(partials, "inner.mustache")
	for path, contents := range map[string]string{
		data:     "name: Ada\n",
		template: "{{>partials/outer.mustache}}",
		outer:    "{{>inner.mustache}}",
		inner:    "Hello {{name}}!",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	exit := run([]string{"--strict", data, template}, strings.NewReader(""), &stdout, &stderr)
	if exit != 0 || stdout.String() != "Hello Ada!" || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunErrorReportsNestedPartialFile(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	partial := filepath.Join(dir, "partial.mustache")
	for path, contents := range map[string]string{
		data:     "{}\n",
		template: "{{>partial.mustache}}",
		partial:  "first\n{{missing}}",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	exit := run([]string{"--strict", data, template}, strings.NewReader(""), &stdout, &stderr)
	wantError := fmt.Sprintf("mustache: render template %s:2: missing tag %q\n", partial, "missing")
	if exit != 1 || stdout.String() != "first\n" || stderr.String() != wantError {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunStrictMissingDynamicPartialName(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(data, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(template, []byte("{{>*missing.partial.mustache}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exit := run([]string{"--strict", data, template}, strings.NewReader(""), &stdout, &stderr)
	wantError := fmt.Sprintf("mustache: render template %s:1: missing tag %q\n", template, "missing.partial.mustache")
	if exit != 1 || stdout.Len() != 0 || stderr.String() != wantError {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunCustomDelimitersAndUsageErrors(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(data, []byte("name: Ada\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(template, []byte("<%name%>"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if exit := run([]string{"--set-delimiters", "<% %>", data, template}, strings.NewReader(""), &stdout, &stderr); exit != 0 || stdout.String() != "Ada" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := run(nil, strings.NewReader(""), &stdout, &stderr); exit != 2 || !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
}

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run([]string{"--version"}, strings.NewReader(""), &stdout, &stderr)
	if exit != 0 || stdout.String() != "mustache dev\n" || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunVerbose(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(data, []byte("name: Ada\nscript: !code {bash: 'printf result; printf warning >&2'}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(template, []byte("Hello {{name}}\n{{script}}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	absTemplate, err := filepath.Abs(template)
	if err != nil {
		t.Fatal(err)
	}

	for _, option := range []string{"-v", "--verbose"} {
		t.Run(option, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			exit := run([]string{option, data, template}, strings.NewReader(""), &stdout, &stderr)
			if exit != 0 || stdout.String() != "Hello Ada\nresult\n" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
			for _, want := range []string{
				"mustache: verbose: reading YAML from " + data,
				"mustache: verbose: decoded YAML document 1; opening template " + template,
				"mustache: verbose: rendering template " + absTemplate,
				"mustache: verbose: lookup \"name\" in " + absTemplate + ":1: found",
				"mustache: verbose: lookup \"script\" in " + absTemplate + ":2: found",
				"mustache: verbose: executing bash command args=[\"bash\" \"-c\" \"printf result; printf warning >&2\"]",
				"mustache: verbose: bash command stdout=\"result\" stderr=\"warning\" exit_code=0",
				"mustache: verbose: finished template " + absTemplate,
			} {
				if !strings.Contains(stderr.String(), want) {
					t.Errorf("stderr does not contain %q:\n%s", want, stderr.String())
				}
			}
		})
	}
}

func TestRunVerboseLogsBashSectionArguments(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(data, []byte("script: !code {bash: 'printf %s \"$1\"'}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(template, []byte("{{#script}}hello world{{/script}}"), 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	exit := run([]string{"--verbose", data, template}, strings.NewReader(""), &stdout, &stderr)
	wantCommand := `"printf %s \"$1\"" "-s" "hello world"`
	if runtime.GOOS == "windows" {
		wantCommand = `"set -- \"$MUSTACHE_LAMBDA_ARG\"; printf %s \"$1\"" "-s" "hello world"`
	}
	wantLog := `mustache: verbose: executing bash command args=["bash" "-c" ` + wantCommand + `]`
	if exit != 0 || stdout.String() != "hello world" || !strings.Contains(stderr.String(), wantLog) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	if err := os.WriteFile(data, []byte("this:\n  is: ok\n  # ok\n\n thatisNotOk!\nyes: right\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(template, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	exit := run([]string{data, template}, strings.NewReader(""), &stdout, &stderr)
	wantError := fmt.Sprintf("mustache: decode YAML %s:5: did not find expected key\n", data)
	if exit != 1 || stdout.Len() != 0 || stderr.String() != wantError {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}
