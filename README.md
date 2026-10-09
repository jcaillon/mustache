# Mustache

A logic-less templating system for HTML, configuration files, and more.

The `mustache` command renders a [Mustache](https://mustache.github.io/) template with data supplied in YAML. It writes the rendered document or documents to standard output. YAML supports lists, mappings, and multiple documents, making it suitable for everything from simple substitutions to mail-merge-style output.

## Contents

- [Contents](#contents)
- [Installation](#installation)
- [Usage](#usage)
- [How it works](#how-it-works)
  - [Data and templates](#data-and-templates)
  - [Multiple YAML documents](#multiple-yaml-documents)
- [Options](#options)
- [Examples](#examples)

## Installation

Install Mustache by downloading a binary from the releases page.

## Usage

```text
mustache [OPTIONS] [--] <YAML> <FILE>
```

Pass a YAML data source followed by a Mustache template file. The rendered result is printed to standard output. Use `-` as the YAML argument to read the data from standard input.

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

## Options

| Option | Description |
| --- | --- |
| `--keep-missing-tags` | Keep missing tags "as is" in the output instead of replacing them with empty strings. |
| `--strict` | In this mode, any missing tags (tags in the template that do not have a corresponding value in the data) will cause the command to fail, indicating the missing tag (in dot notation). |
| `--set-delimiters` | Set custom delimiters for the template. The opening and closing delimiters should be separated by a whitespace. For example, `--set-delimiters "{{ }}"` sets the delimiters to the default Mustache delimiters. |

## Examples

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
