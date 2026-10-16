package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type cliSpec struct {
	Overview string    `yaml:"overview"`
	Tests    []cliTest `yaml:"tests"`
}

type cliTest struct {
	Name           string            `yaml:"name"`
	Desc           string            `yaml:"desc"`
	Args           []string          `yaml:"args"`
	Files          []cliFile         `yaml:"files"`
	Env            map[string]string `yaml:"env"`
	Stdin          string            `yaml:"stdin"`
	Stdout         string            `yaml:"stdout"`
	Stderr         string            `yaml:"stderr"`
	StderrContains []string          `yaml:"stderr_contains"`
	StderrPattern  string            `yaml:"stderr_pattern"`
	ExitCode       int               `yaml:"exit_code"`
	AbsentFiles    []string          `yaml:"absent_files"`
}

type cliFile struct {
	Path    string `yaml:"path"`
	Content string `yaml:"content"`
}

func TestCLI(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("cli-tests", "*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no CLI specifications found")
	}
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var spec cliSpec
		decoder := yaml.NewDecoder(bytes.NewReader(contents))
		decoder.KnownFields(true)
		if err := decoder.Decode(&spec); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if len(spec.Tests) == 0 {
			t.Fatalf("%s: no tests found", file)
		}
		for _, test := range spec.Tests {
			t.Run(filepath.Base(file)+"/"+test.Name, func(t *testing.T) {
				dir := t.TempDir()
				t.Chdir(dir)
				expand := strings.NewReplacer("${DIR}/", dir+string(os.PathSeparator), "${DIR}", dir).Replace
				for _, file := range test.Files {
					if !filepath.IsLocal(file.Path) {
						t.Fatalf("fixture path must be relative to the test directory: %q", file.Path)
					}
					path := filepath.FromSlash(file.Path)
					if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte(expand(file.Content)), 0o600); err != nil {
						t.Fatal(err)
					}
				}
				for name, value := range test.Env {
					t.Setenv(name, expand(value))
				}
				args := make([]string, len(test.Args))
				for i, arg := range test.Args {
					args[i] = expand(arg)
				}
				var stdout, stderr bytes.Buffer
				exit := run(args, strings.NewReader(expand(test.Stdin)), &stdout, &stderr)
				if exit != test.ExitCode {
					t.Errorf("exit code: want %d, got %d\nstdout: %q\nstderr: %q", test.ExitCode, exit, stdout.String(), stderr.String())
				}
				if want := expand(test.Stdout); stdout.String() != want {
					t.Errorf("stdout mismatch\nwant: %q\n got: %q", want, stdout.String())
				}
				if len(test.StderrContains) == 0 && test.StderrPattern == "" {
					if want := expand(test.Stderr); stderr.String() != want {
						t.Errorf("stderr mismatch\nwant: %q\n got: %q", want, stderr.String())
					}
				}
				for _, part := range test.StderrContains {
					if want := expand(part); !strings.Contains(stderr.String(), want) {
						t.Errorf("stderr does not contain %q:\n%s", want, stderr.String())
					}
				}
				if test.StderrPattern != "" {
					pattern, err := regexp.Compile(test.StderrPattern)
					if err != nil {
						t.Fatal(err)
					}
					if !pattern.MatchString(stderr.String()) {
						t.Errorf("stderr does not match %q:\n%s", test.StderrPattern, stderr.String())
					}
				}
				for _, path := range test.AbsentFiles {
					if _, err := os.Stat(expand(path)); !os.IsNotExist(err) {
						t.Errorf("file must not exist: %s (stat error: %v)", path, err)
					}
				}
			})
		}
	}
}
