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

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func terminalSupportsColor() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	switch os.Getenv("TERM") {
	case "xterm-color", "xterm-16color", "xterm-88color", "xterm-256color", "linux", "rxvt-16color", "konsole-16color":
		return true
	default:
		return os.Getenv("COLORTERM") != ""
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("mustache", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var baseData, overrideData []string
	flags.Func("base-data", "merge YAML `FILE` before the main data (repeatable)", func(name string) error {
		baseData = append(baseData, name)
		return nil
	})
	disableHTMLEscape := flags.Bool("disable-html-escape", false, "do not HTML-escape tag values")
	disableLambda := flags.Bool("disable-lambda", false, "fail if a lambda is invoked, preventing Bash command execution")
	keepMissing := flags.Bool("keep-missing-tags", false, "keep missing tags in the output")
	outputSeparator := flags.String("output-separator", "", "write `TEXT` between rendered YAML documents (default: no separator)")
	flags.Func("override-data", "merge YAML `FILE` after the main data (repeatable)", func(name string) error {
		overrideData = append(overrideData, name)
		return nil
	})
	runCount := flags.Int("run-count", 1, "render `N` times, using each result as the next template")
	strict := flags.Bool("strict", false, "fail when a tag or partial file is missing")
	setDelimiters := flags.String("set-delimiters", "{{ }}", "opening and closing `DELIMITERS` separated by whitespace")
	showVersion := flags.Bool("version", false, "print the version and exit")
	var verbose bool
	flags.BoolVar(&verbose, "verbose", false, "log rendering details to standard error")
	flags.BoolVar(&verbose, "v", false, "log rendering details to standard error (shorthand)")
	flags.Usage = func() {
		heading, accent, reset := "", "", ""
		if terminalSupportsColor() {
			heading, accent, reset = "\x1b[1;36m", "\x1b[36m", "\x1b[0m"
		}
		fmt.Fprintf(stderr, "%smustache%s - render Mustache templates with YAML data\n\n", heading, reset)
		fmt.Fprintf(stderr, "%sUsage:%s\n  mustache [OPTIONS] [--] <YAML> <FILE>\n\n", heading, reset)
		fmt.Fprintf(stderr, "%sArguments:%s\n", heading, reset)
		fmt.Fprintln(stderr, "  YAML  YAML data file, or - to read from standard input")
		fmt.Fprintln(stderr, "  FILE  Mustache template file")
		fmt.Fprintln(stderr, "\nRendered output is written to standard output.")
		fmt.Fprintf(stderr, "\n%sOptions:%s\n", heading, reset)
		flags.VisitAll(func(option *flag.Flag) {
			if option.Name == "v" {
				return
			}
			name := "--" + option.Name
			if option.Name == "verbose" {
				name = "-v, " + name
			}
			value, description := flag.UnquoteUsage(option)
			if value != "" {
				name += " " + value
			}
			if option.DefValue != "" && option.DefValue != "false" {
				description += fmt.Sprintf(" (default: %q)", option.DefValue)
			}
			fmt.Fprintf(stderr, "  %s%s%s\n      %s\n", accent, name, reset, description)
		})
		fmt.Fprintf(stderr, "  %s-h, --help%s\n      show this help and exit\n", accent, reset)
		fmt.Fprintf(stderr, "\n%sExamples:%s\n", heading, reset)
		fmt.Fprintln(stderr, "  mustache data.yml template.mustache")
		fmt.Fprintln(stderr, "  cat data.yml | mustache - template.mustache")
		fmt.Fprintf(stderr, "\n%sEnvironment:%s\n", heading, reset)
		fmt.Fprintln(stderr, "  NO_COLOR  Set to a non-empty value to disable colors.")
		fmt.Fprintf(stderr, "\n%sFull documentation:%s\n  %shttps://github.com/jcaillon/mustache%s\n", heading, reset, accent, reset)
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if verbose {
		fmt.Fprintf(stderr, "mustache: verbose: arguments=%q\n", args)
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
	if *runCount < 1 {
		fmt.Fprintln(stderr, "mustache: --run-count must be at least 1")
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
		Missing:           missing,
		Delimiters:        [2]string{delimiters[0], delimiters[1]},
		Verbose:           verboseOutput,
		DisableHTMLEscape: *disableHTMLEscape,
		DisableLambda:     *disableLambda,
	})
	if err != nil {
		fmt.Fprintf(stderr, "mustache: %v\n", err)
		return 2
	}
	baseDocuments, err := render.LoadDataFiles(baseData)
	if err != nil {
		fmt.Fprintf(stderr, "mustache: load base data: %v\n", err)
		return 1
	}
	overrideDocuments, err := render.LoadDataFiles(overrideData)
	if err != nil {
		fmt.Fprintf(stderr, "mustache: load override data: %v\n", err)
		return 1
	}

	if verbose {
		fmt.Fprintf(verboseOutput, "mustache: verbose: reading YAML from %s\n", flags.Arg(0))
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
			fmt.Fprintf(stderr, "mustache: decode YAML %s\n", render.FormatYAMLParserError(flags.Arg(0), err))
			return 1
		}
		decoded = true
		if render.EmptyYAMLDocument(&documentNode) {
			continue
		}
		document, err := render.DecodeData(&documentNode)
		if err != nil {
			fmt.Fprintf(stderr, "mustache: decode YAML %s: %v\n", flags.Arg(0), err)
			return 1
		}
		var merged any
		for _, base := range baseDocuments {
			merged = render.MergeData(merged, base)
		}
		merged = render.MergeData(merged, document)
		for _, override := range overrideDocuments {
			merged = render.MergeData(merged, override)
		}
		documentNumber++
		if verbose {
			fmt.Fprintf(verboseOutput, "mustache: verbose: decoded YAML document %d; opening template %s\n", documentNumber, flags.Arg(1))
		}
		template, err := os.Open(flags.Arg(1))
		if err != nil {
			fmt.Fprintf(stderr, "mustache: open template: %v\n", err)
			return 1
		}
		if rendered && *outputSeparator != "" {
			_, err = io.WriteString(stdout, *outputSeparator)
		}
		if err == nil {
			var input io.Reader = template
			for pass := 0; pass < *runCount; pass++ {
				var intermediate strings.Builder
				var output io.Writer = stdout
				if pass < *runCount-1 {
					output = &intermediate
				}
				if err = renderer.Render(output, input, flags.Arg(1), merged); err != nil {
					break
				}
				if pass < *runCount-1 {
					input = strings.NewReader(intermediate.String())
				}
			}
		}
		closeErr := template.Close()
		if err != nil {
			var renderErr *render.Error
			if errors.As(err, &renderErr) {
				fmt.Fprintf(stderr, "mustache: render template %s:%d: %v\n", renderErr.File, renderErr.Line, renderErr.Err)
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
