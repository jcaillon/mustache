package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jcaillon/mustache/internal/render"
	"gopkg.in/yaml.v3"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("mustache", flag.ContinueOnError)
	flags.SetOutput(stderr)
	allowMissing := flags.Bool("allow-missing-tags", false, "keep missing tags in the output")
	setDelimiters := flags.String("set-delimiters", "{{ }}", "opening and closing delimiters separated by whitespace")
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
	if flags.NArg() != 2 {
		flags.Usage()
		return 2
	}

	delimiters := strings.Fields(*setDelimiters)
	if len(delimiters) != 2 {
		fmt.Fprintln(stderr, "mustache: --set-delimiters requires two delimiters separated by whitespace")
		return 2
	}
	missing := render.MissingError
	if *allowMissing {
		missing = render.MissingKeep
	}
	renderer, err := render.New(render.Options{
		Missing:    missing,
		Delimiters: [2]string{delimiters[0], delimiters[1]},
	})
	if err != nil {
		fmt.Fprintf(stderr, "mustache: %v\n", err)
		return 2
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
	for {
		var documentNode yaml.Node
		err := decoder.Decode(&documentNode)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			fmt.Fprintf(stderr, "mustache: decode YAML: %v\n", err)
			return 1
		}
		if len(documentNode.Content) == 0 || documentNode.Content[0].Tag == "!!null" && documentNode.Content[0].Value == "" {
			continue
		}
		var document any
		if err := documentNode.Decode(&document); err != nil {
			fmt.Fprintf(stderr, "mustache: decode YAML: %v\n", err)
			return 1
		}
		template, err := os.Open(flags.Arg(1))
		if err != nil {
			fmt.Fprintf(stderr, "mustache: open template: %v\n", err)
			return 1
		}
		err = renderer.Render(stdout, template, document)
		closeErr := template.Close()
		if err != nil {
			fmt.Fprintf(stderr, "mustache: render template: %v\n", err)
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
