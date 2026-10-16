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

func TestRunMergesBaseMainAndOverrideData(t *testing.T) {
	dir := t.TempDir()
	baseOne := filepath.Join(dir, "base-one, shared.yml")
	baseTwo := filepath.Join(dir, "base-two.yml")
	baseThree := filepath.Join(dir, "base-third.yml")
	mainData := filepath.Join(dir, "main.yml")
	overrideOne := filepath.Join(dir, "override-one, shared.yml")
	overrideTwo := filepath.Join(dir, "override-two.yml")
	overrideThree := filepath.Join(dir, "override-three.yml")
	template := filepath.Join(dir, "template.mustache")
	for path, contents := range map[string]string{
		baseOne:       "baseOnly: base\nbaseOrder: base-one\norder: base-one\nnested:\n  fromBase: yes\n  order: base-one\narray: [base-one]\n",
		baseTwo:       "baseOrder: base-two\norder: base-two\nnested:\n  order: base-two\n  fromBaseTwo: yes\narray: [base-two]\n",
		baseThree:     "",
		mainData:      "---\nname: first\norder: main\nnested:\n  order: main\n  fromMain: one\narray: [main-one, main-two]\n---\nname: second\nnested:\n  fromMain: two\narray: [main-three]\n",
		overrideOne:   "order: override-one\nnested:\n  order: override-one\n  fromOverride: yes\narray: [override-one]\n",
		overrideTwo:   "order: override-two\nnested:\n  order: override-two\noverrideOnly: override\narray: [override-two, override-three]\n",
		overrideThree: "",
		template:      "{{name}}|{{baseOnly}}|{{baseOrder}}|{{order}}|{{nested.fromBase}}|{{nested.fromBaseTwo}}|{{nested.fromMain}}|{{nested.fromOverride}}|{{overrideOnly}}|{{#array}}{{.}},{{/array}}\n",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	exit := run([]string{
		"--base-data", baseOne,
		"--base-data", baseTwo,
		"--base-data", baseThree,
		"--override-data", overrideOne,
		"--override-data", overrideTwo,
		"--override-data", overrideThree,
		mainData,
		template,
	}, strings.NewReader(""), &stdout, &stderr)
	want := "first|base|base-two|override-two|yes|yes|one|yes|override|override-two,override-three,\n" +
		"second|base|base-two|override-two|yes|yes|two|yes|override|override-two,override-three,\n"
	if exit != 0 || stdout.String() != want || stderr.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

func TestRunRejectsMultipleDocumentsInAdditionalData(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	mainData := filepath.Join(dir, "main.yml")
	template := filepath.Join(dir, "template.mustache")
	for path, contents := range map[string]string{
		base:     "---\none: 1\n---\ntwo: 2\n",
		mainData: "{}\n",
		template: "unused",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	exit := run([]string{"--base-data", base, mainData, template}, strings.NewReader(""), &stdout, &stderr)
	wantError := fmt.Sprintf("mustache: load base data: YAML %s contains more than one document\n", base)
	if exit != 1 || stdout.Len() != 0 || stderr.String() != wantError {
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

func TestRunDisableLambda(t *testing.T) {
	dir := t.TempDir()
	data := filepath.Join(dir, "data.yml")
	template := filepath.Join(dir, "template.mustache")
	partial := filepath.Join(dir, "partial.mustache")
	marker := filepath.Join(dir, "lambda-executed")
	t.Setenv("MUSTACHE_TEST_LAMBDA_MARKER", filepath.ToSlash(marker))
	input := "value: safe\nenabled: true\nlambda: &lambda !code {bash: 'printf executed > \"$MUSTACHE_TEST_LAMBDA_MARKER\"; printf result'}\nalias: *lambda\n"
	for path, contents := range map[string]string{
		data:    input,
		partial: "{{lambda}}",
	} {
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	sources := []struct {
		name  string
		args  []string
		input string
	}{
		{name: "stdin", args: []string{"-", template}, input: input},
		{name: "file", args: []string{data, template}},
		{name: "base", args: []string{"--base-data", data, "-", template}, input: "{}"},
		{name: "override", args: []string{"--override-data", data, "-", template}, input: "{}"},
	}
	tests := []struct {
		name     string
		template string
		exit     int
		output   string
	}{
		{name: "interpolation", template: "{{lambda}}", exit: 1},
		{name: "unescaped", template: "{{{lambda}}}", exit: 1},
		{name: "ampersand", template: "{{&lambda}}", exit: 1},
		{name: "indented", template: "{{|lambda}}", exit: 1},
		{name: "section", template: "{{#lambda}}text{{/lambda}}", exit: 1},
		{name: "inverted", template: "{{^lambda}}text{{/lambda}}", exit: 1},
		{name: "conditional", template: "{{?if lambda==result}}text{{?fi}}", exit: 1},
		{name: "elif", template: "{{?if value==other}}text{{?elif lambda==result}}text{{?fi}}", exit: 1},
		{name: "nested", template: "{{#enabled}}{{lambda}}{{/enabled}}", exit: 1},
		{name: "partial", template: "{{>partial.mustache}}", exit: 1},
		{name: "alias", template: "{{alias}}", exit: 1},
		{name: "unused", template: "{{value}}", output: "safe"},
		{name: "skipped section", template: "{{#missing}}{{lambda}}{{/missing}}{{value}}", output: "safe"},
		{name: "skipped branch", template: "{{?if value==safe}}{{value}}{{?elif lambda==result}}{{lambda}}{{?fi}}", output: "safe"},
	}
	for _, source := range sources {
		for _, test := range tests {
			t.Run(source.name+"/"+test.name, func(t *testing.T) {
				if err := os.WriteFile(template, []byte(test.template), 0o600); err != nil {
					t.Fatal(err)
				}
				var stdout, stderr bytes.Buffer
				args := append([]string{"--disable-lambda"}, source.args...)
				exit := run(args, strings.NewReader(source.input), &stdout, &stderr)
				if exit != test.exit || stdout.String() != test.output {
					t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
				}
				if test.exit == 1 {
					if !strings.Contains(stderr.String(), "lambda usage is disabled (--disable-lambda)") {
						t.Fatalf("expected disabled-lambda error, got %q", stderr.String())
					}
				} else if stderr.Len() != 0 {
					t.Fatalf("unexpected stderr: %q", stderr.String())
				}
				if _, err := os.Stat(marker); !os.IsNotExist(err) {
					t.Fatalf("lambda must not execute: marker stat error=%v", err)
				}
			})
		}
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
			args := []string{option, data, template}
			exit := run(args, strings.NewReader(""), &stdout, &stderr)
			if exit != 0 || stdout.String() != "Hello Ada\nresult\n" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
			for _, want := range []string{
				fmt.Sprintf("mustache: verbose: arguments=%q\n", args),
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

func TestRunVerboseLogsArgumentsBeforeDataErrors(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing data.yml")
	for _, option := range []string{"-v", "--verbose"} {
		t.Run(option, func(t *testing.T) {
			args := []string{option, "--base-data", missing, "--base-data", "/c/base.yml", "-", "template.mustache"}
			var stdout, stderr bytes.Buffer
			exit := run(args, strings.NewReader(""), &stdout, &stderr)
			wantLog := fmt.Sprintf("mustache: verbose: arguments=%q\n", args)
			if exit != 1 || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), wantLog) || !strings.Contains(stderr.String(), "mustache: load base data:") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
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
