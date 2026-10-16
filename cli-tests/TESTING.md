# CLI tests

`main_test.go` loads every `cli-tests/*.yml` file and calls `run` for each entry
in its `tests` list. Like the renderer specifications in `specs/`, suites have
an `overview`, and tests have a `name` and optional `desc`.

```yaml
overview: Read YAML from stdin and render a template.
tests:
  - name: Greeting
    args: ["-", template.mustache]
    stdin: "name: Ada\n"
    files:
      - path: template.mustache
        content: "Hello {{name}}!\n"
    stdout: "Hello Ada!\n"
    stderr: ""
    exit_code: 0
```

Each test runs in its own temporary working directory. `files` is a list of
relative paths and their exact contents; parent directories are created
automatically. YAML data files are strings, so document separators, invalid
YAML, aliases, and `!code` tags reach the CLI unchanged. `args` is passed as a
list, without shell parsing or splitting.

Optional properties:

| Property | Meaning | Default |
| --- | --- | --- |
| `stdin` | Exact input passed to `run` | Empty |
| `stdout` | Exact expected standard output | Empty |
| `stderr` | Exact expected standard error | Empty |
| `exit_code` | Expected return value | `0` |
| `env` | Environment variables set for this test and restored afterward | None |
| `absent_files` | Paths that must not exist after running, for lambda safety checks | None |
| `stderr_contains` | List of required substrings, instead of an exact stderr comparison | None |
| `stderr_pattern` | Go regular expression to match stderr, instead of an exact comparison | None |

`stderr_contains` and `stderr_pattern` can be combined; both must match when
provided. Regular expressions are unanchored unless explicitly anchored.
Without either matcher, stderr must equal `stderr`, including when it is empty.

`${DIR}` expands to the temporary directory in arguments, fixture contents,
environment values, stdin, expected output, stderr substrings, and absent-file
paths. `${DIR}/` uses the native path separator, so absolute-path expectations
work on Windows as well as Unix. Regular expressions are used as written.
YAML anchors and merge keys can share fixtures and expectations between tests.

Run all tests with `go test ./...`, or just the CLI suite with:

```sh
go test . -run '^TestCLI$'
```

`bash cli-tests/run.sh` invokes the same CLI suite and accepts additional
`go test` flags, such as `-v` or `-count=1`. It fails on assertion failures and
does not overwrite expected output.
