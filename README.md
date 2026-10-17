# Mustache

A logic-less templating system for HTML, configuration files, and more.

The `mustache` command renders a [Mustache](https://mustache.github.io/) template with data supplied in YAML. It writes the rendered document or documents to standard output. YAML supports lists, mappings, and multiple documents, making it suitable for everything from simple substitutions to mail-merge-style output.

> [!IMPORTANT]
> Disclaimer: This tool was carefully reviewed and tested, but it was coded using IA.

## Contents

- [Contents](#contents)
- [Installation](#installation)
- [Usage](#usage)
- [How it works](#how-it-works)
  - [Data and templates](#data-and-templates)
  - [Multiple YAML documents](#multiple-yaml-documents)
- [Options](#options)
- [Examples](#examples)
  - [Lambdas](#lambdas)
- [Mustache extensions](#mustache-extensions)
  - [Indented interpolation](#indented-interpolation)
  - [Conditional rendering](#conditional-rendering)
- [Development](#development)
- [Releasing](#releasing)

## Installation

Install Mustache by downloading an archive for your platform from the [releases page](https://github.com/jcaillon/mustache/releases). Extract the archive and place the `mustache` executable in a directory on your `PATH`.

You can also install the latest source with Go:

```sh
go install github.com/jcaillon/mustache@latest
```

## Usage

```text
mustache [OPTIONS] [--] <YAML> <FILE>
```

Pass a YAML data source followed by a Mustache template file. The rendered result is printed to standard output. Use `-` as the YAML argument to read the data from standard input.

Run `mustache --help` for options and examples. Help uses colors when `TERM` identifies a supported color terminal or `COLORTERM` is non-empty. Set `NO_COLOR` to any non-empty value to disable colors.

Use `--base-data` and `--override-data` to merge additional single-document YAML files into each document from the main data source. Each option accepts one file path; repeat the option for each additional file. Files are merged in this order: base files in option order, the current main document, then override files in option order. Mappings are merged recursively; all other values, including lists, replace the earlier value. Mustache will still render multiple times depending on the number of documents in the main data source.

## How it works

Mustache templates use tags such as `{{name}}` to insert values and sections such as `{{#names}} ... {{/names}}` to iterate over a collection. Mustache is logic-less: template structure is driven by the supplied data rather than embedded application logic.

For an overview of template syntax, see the [Mustache manual](https://mustache.github.io/mustache.5.html).

By default, if a tag in the template does not have a corresponding value in the data, it will be considered as a null value (rendered as an empty string or as a falsey value). You can override this behavior with the `--keep-missing-tags` or `--strict` option.

### Data and templates

A YAML data file can contain a single document:

```yaml
---
names:
  - name: chris
  - name: mark
  - name: scott
---
```

The template can iterate over the `names` collection:

```mustache
{{#names}}
Hi {{name}}!
{{/names}}
```

Render the template by passing the data file first and the template second:

```sh
mustache data.yml template.mustache
```

Output:

```text
Hi chris!
Hi mark!
Hi scott!
```

If you're unfamiliar with YAML, valid JSON is also valid YAML, so JSON-formatted data can be used as well.

### Multiple YAML documents

Separate YAML documents with `---` to render the template once for each document. This is useful for mail-merge-style output.

For example, `data.yml`:

```yaml
---
name: chris
---
name: mark
---
name: scott
---
```

And `template.mustache`:

```mustache
Hi {{name}}!
```

Run:

```sh
mustache data.yml template.mustache
```

Output:

```text
Hi chris!
Hi mark!
Hi scott!
```

By default, rendered documents are concatenated without a separator. Use `--output-separator` to insert a literal string between them, with no leading or trailing separator. For example, in Bash, a single-document YAML template can produce multi-document YAML:

```sh
mustache --output-separator $'\n---\n' data.yml template.yml.mustache
```

## Options

| Option | Description |
| --- | --- |
| `--base-data` | Single-document YAML file to merge before each main data document. Repeat for multiple files, merged in the given order. |
| `--disable-html-escape` | Do not HTML-escape tag values. |
| `--disable-lambda` | Fail if a lambda is invoked, preventing Bash command execution from YAML data. Use when the data is untrusted. |
| `--keep-missing-tags` | Keep missing tags "as is" in the output instead of replacing them with empty strings. |
| `--output-separator` | String written between rendered documents. Defaults to an empty string (no separator). |
| `--override-data` | Single-document YAML file to merge after each main data document. Repeat for multiple files, merged in the given order. |
| `--run-count N` | Render each document `N` times, using each result as the next template with the same data. Defaults to `1`; must be at least `1`. Only the final result is printed. |
| `--strict` | Fail on missing tags or partial files. Missing partial errors include the full expected file path. |
| `--set-delimiters` | Set custom delimiters for the template. The opening and closing delimiters should be separated by a whitespace. For example, `--set-delimiters "{{ }}"` sets the delimiters to the default Mustache delimiters. |
| `-v`, `--verbose` | Log rendering steps, contextual lookups, partials, and Bash lambda execution details to standard error. |
| `--version` | Print the version and exit. |

## Examples

For a compact syntax reference with one data file, an annotated template and expected output, see the [demo](demo/README.md).

Render a template from files:

```sh
mustache data.yml template.mustache
```

Read YAML data from standard input:

```sh
cat data.yml | mustache - template.mustache
```

Pass YAML data through a heredoc:

```sh
cat <<'data' | mustache - template.mustache
---
name: Bob
age: 30
---
data
```

Render a template while preserving missing tags:

```sh
mustache --keep-missing-tags data.yml template.mustache
```

Render a template with custom delimiters:

```sh
mustache --set-delimiters "<% %>" data.yml template.mustache
```

Merge defaults and environment-specific overrides into every main data document:

```sh
mustache --base-data defaults.yml --base-data shared.yml --override-data production.yml data.yml template.mustache
```

Use multiple passes to resolve indirect data references. Given `data.yml`:

```yaml
open: '{{'
close: '}}'
choice: prod
env:
  prod: production
  qa: quality assurance
```

And `template.mustache`:

```mustache
env: <{{open}}env.{{choice}}{{close}}>
```

Run:

```sh
mustache --run-count 2 --disable-html-escape data.yml template.mustache
```

The first pass produces `env: <{{env.prod}}>`; the second prints `env: <production>`.
Each pass uses the same options and merged data. Intermediate results are held in memory; template files are not modified.

### Lambdas

Lambdas let YAML data run Bash commands during rendering. Define a value with `!code` and a `bash` command, for example `greeting: !code {bash: 'printf world'}`. `{{greeting}}` runs it without arguments; `{{#greeting}}...{{/greeting}}` passes the raw section text as `$1`. Standard output is rendered as a Mustache template using default delimiters for interpolation and current delimiters for sections. A nonzero exit status fails interpolation or renders a section as empty. Bash must be available; only render trusted YAML because commands execute on your machine. See the [lambda specification and examples](specs/lambdas.yml) for details.

Use `mustache --disable-lambda data.yml template.mustache` when YAML data is untrusted. Any attempt to invoke a lambda fails before Bash starts, including lambdas from base and override files. Unused lambda definitions and lambdas in skipped branches do not cause errors.

## Mustache extensions

The following features extend the original Mustache specification and may not be supported by other Mustache implementations.

### Indented interpolation

Use `{{|name}}` instead of `{{name}}` to align continuation lines of a multiline value with the tag's position in the rendered output. Regular interpolation leaves continuation lines unindented.

For example, with this YAML data:

```yaml
text: |-
  First line.
  Second line.
```

And this template:

```mustache
message: {{|text}}
```

The output is:

```text
message: First line.
         Second line.
```

Indentation is based on the current rendered line, including output from earlier tags, rather than the tag's position in the template source. Spaces and tabs in that prefix are preserved; other characters are replaced with spaces. Continuation lines, including blank lines within the value, receive this prefix in addition to any existing whitespace. A final newline does not add trailing indentation or affect following template text.

Name lookup, string coercion, HTML escaping, lambda expansion, and missing-tag handling work as in regular interpolation. LF and CRLF line endings are preserved.

See the [indented interpolation specification and test cases](specs/extra-indented-interpolation.yml) for the complete behavior and more examples.

### Conditional rendering

Use `{{?if name==literal}}`, optional `{{?elif name==literal}}` branches, `{{?else}}`, and `{{?fi}}` to render only the first matching branch:

```mustache
{{?if environment==production}}
Production configuration
{{?elif environment=~^qa}}
QA configuration
{{?else}}
Other configuration
{{?fi}}
```

The left operand uses normal Mustache lookup and string coercion without HTML escaping. The right operand is literal text, not a data lookup. Operators are `==`, `!=`, `=~` (regular expression match), and `!~` (negated match). Regular expressions use [Go's syntax](https://pkg.go.dev/regexp/syntax) and match any part of the value unless anchored.

Blocks may be nested and do not change the current context. Skipped branches are not evaluated. Standalone control-tag lines are removed, just like section-tag lines. Missing operands compare as empty strings by default, fail with `--strict`, and preserve the entire block unchanged with `--keep-missing-tags`.

See the [conditional rendering specification and test cases](specs/extra-conditional.yml) for the complete behavior.

## Development

Run the same checks used by CI:

```sh
go vet ./...
go test ./...
go build ./...
```

CLI behavior is defined in the YAML suites under [`cli-tests/`](cli-tests/TESTING.md)
and run by `main_test.go`. Renderer specifications live under [`specs/`](specs/TESTING.md).

To validate the release configuration without publishing anything:

```sh
goreleaser check
goreleaser release --snapshot --clean
```

## Releasing

Releases use semantic version tags. Make sure CI is green on `main`, then create and push an annotated tag:

```sh
git switch main
git pull --ff-only
git tag -a v1.0.0 -m "Release v1.0.0"
git push origin v1.0.0
```

Pushing the tag runs the release workflow. It tests the project, builds Linux, macOS, and Windows archives for amd64 and arm64, generates SHA-256 checksums and release notes, and publishes them to GitHub Releases. Tags such as `v1.1.0-rc.1` are published as prereleases.
