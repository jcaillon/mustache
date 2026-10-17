# Mustache quick reference

Read [template.mustache](template.mustache) alongside the single [data.yml](data.yml).
Seven short sections cover the main syntax; [card.mustache](card.mustache) is reused
for partials, dynamic names and block overrides.

From the repository root:

```sh
mustache demo/data.yml demo/template.mustache
```

Or run the source with `go run . demo/data.yml demo/template.mustache`.
Compare with [expected.txt](expected.txt), or check it in Bash:

```sh
mustache demo/data.yml demo/template.mustache | diff -u demo/expected.txt -
```

- Comments explain the tags without appearing in the output. Standalone section,
  comment and control-tag lines are removed; ordinary text keeps its whitespace.
- Dynamic names, inheritance and lambdas are optional Mustache features.
  This project's `!code` YAML binding executes Bash commands, so Bash must be
  available and the data must be trusted. `--disable-lambda` rejects these calls.
- Indented interpolation and conditional branches are project-specific extensions.
  Conditional regexes match anywhere unless anchored; the example uses `^qa`.
- Try `enabled: false`, `alerts: [warning]` or `environment: production` to see
  section and branch selection change. The missing-variable example intentionally
  uses default behavior, so `--strict` will fail on it.
