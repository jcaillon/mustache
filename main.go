package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/jcaillon/mustache/internal/render"
	"gopkg.in/yaml.v3"
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("mustache", flag.ContinueOnError)
	flags.SetOutput(stderr)
	keepMissing := flags.Bool("keep-missing-tags", false, "keep missing tags in the output")
	strict := flags.Bool("strict", false, "fail when a tag or partial file is missing")
	setDelimiters := flags.String("set-delimiters", "{{ }}", "opening and closing delimiters separated by whitespace")
	showVersion := flags.Bool("version", false, "print the version and exit")
	var verbose bool
	flags.BoolVar(&verbose, "verbose", false, "log rendering details to standard error")
	flags.BoolVar(&verbose, "v", false, "log rendering details to standard error (shorthand)")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mustache [OPTIONS] [--] <YAML> <FILE>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(stdout, "mustache", version)
		return 0
	}
	if flags.NArg() != 2 {
		fmt.Fprintln(stderr, "mustache: wrong number of arguments (expected 2, got", flags.NArg(), ")")
		fmt.Fprintln(stderr, "Arguments provided:", strings.Join(args, " "))
		flags.Usage()
		return 2
	}
	if *keepMissing && *strict {
		fmt.Fprintln(stderr, "mustache: --keep-missing-tags and --strict cannot be used together")
		return 2
	}

	delimiters := strings.Fields(*setDelimiters)
	if len(delimiters) != 2 {
		fmt.Fprintln(stderr, "mustache: --set-delimiters requires two delimiters separated by whitespace")
		return 2
	}
	missing := render.MissingEmpty
	if *keepMissing {
		missing = render.MissingKeep
	} else if *strict {
		missing = render.MissingError
	}
	var verboseOutput io.Writer
	if verbose {
		verboseOutput = stderr
	}
	renderer, err := render.New(render.Options{
		Missing:    missing,
		Delimiters: [2]string{delimiters[0], delimiters[1]},
		Verbose:    verboseOutput,
	})
	if err != nil {
		fmt.Fprintf(stderr, "mustache: %v\n", err)
		return 2
	}

	if verbose {
		fmt.Fprintf(stderr, "mustache: verbose: reading YAML from %s\n", flags.Arg(0))
	}
	data, closeData, err := openData(flags.Arg(0), stdin)
	if err != nil {
		fmt.Fprintf(stderr, "mustache: %v\n", err)
		return 1
	}
	if closeData != nil {
		defer closeData.Close()
	}

	decoder := yaml.NewDecoder(data)
	rendered := false
	decoded := false
	documentNumber := 0
	for {
		var documentNode yaml.Node
		err := decoder.Decode(&documentNode)
		if errors.Is(err, io.EOF) {
			if decoded {
				break
			}
			documentNode = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
			err = nil
		}
		if err != nil {
			fmt.Fprintf(stderr, "mustache: decode YAML %s\n", formatYAMLParserError(flags.Arg(0), err))
			return 1
		}
		decoded = true
		if len(documentNode.Content) == 0 || documentNode.Content[0].Tag == "!!null" && documentNode.Content[0].Value == "" {
			continue
		}
		document, err := render.DecodeData(&documentNode)
		if err != nil {
			fmt.Fprintf(stderr, "mustache: decode YAML %s: %v\n", flags.Arg(0), err)
			return 1
		}
		documentNumber++
		if verbose {
			fmt.Fprintf(stderr, "mustache: verbose: decoded YAML document %d; opening template %s\n", documentNumber, flags.Arg(1))
		}
		template, err := os.Open(flags.Arg(1))
		if err != nil {
			fmt.Fprintf(stderr, "mustache: open template: %v\n", err)
			return 1
		}
		err = renderer.Render(stdout, template, flags.Arg(1), document)
		closeErr := template.Close()
		if err != nil {
			var renderErr *render.Error
			if errors.As(err, &renderErr) {
				fmt.Fprintf(stderr, "mustache: render template %s:%d: %v\n", flags.Arg(1), renderErr.Line, renderErr.Err)
			} else {
				fmt.Fprintf(stderr, "mustache: render template: %v\n", err)
			}
			return 1
		}
		if closeErr != nil {
			fmt.Fprintf(stderr, "mustache: close template: %v\n", closeErr)
			return 1
		}
		rendered = true
	}
	if !rendered {
		fmt.Fprintln(stderr, "mustache: YAML input contains no documents")
		return 1
	}
	return 0
}

func formatYAMLParserError(name string, err error) string {
	const prefix = "yaml: line "

	message := err.Error()
	rest, ok := strings.CutPrefix(message, prefix)
	if !ok {
		return fmt.Sprintf("%s: %s", name, message)
	}
	lineText, message, ok := strings.Cut(rest, ": ")
	if !ok {
		return fmt.Sprintf("%s: %s", name, err)
	}
	line, parseErr := strconv.Atoi(lineText)
	if parseErr != nil {
		return fmt.Sprintf("%s: %s", name, err)
	}
	return fmt.Sprintf("%s:%d: %s", name, line+1, message)
}

func openData(name string, stdin io.Reader) (io.Reader, *os.File, error) {
	if name == "-" {
		return stdin, nil, nil
	}
	file, err := os.Open(name)
	if err != nil {
		return nil, nil, fmt.Errorf("open YAML: %w", err)
	}
	return file, file, nil
}
