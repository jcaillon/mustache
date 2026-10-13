package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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
