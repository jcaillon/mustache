# how to test Mustache implementation

In general, the process for each `.yml` file is as follows:

1. Use a YAML parser to load the file.
2. For each test in the `tests` array:
    1. Ensure that each element of the `partials` hash (if it exists) is
       stored in a place where the interpreter will look for it.
    2. For the `~lambdas.yml` file, ensure that each member of `data` tagged with `!code` is
       properly processed into a language-specific lambda reference.
       e.g. Given this YAML data hash:

       `{ x: !code { bash: 'echo -n "x"', go: 'func() string { return "x" }' } }`
       a Go-based Mustache implementation would process it such that it
       was equivalent to this Go hash:

       `{ "x": func() string { return "x" } }`

       and a bash-based Mustache implementation would process as:

       `bash -c 'echo -n "x"'`
    3. Render the template (stored in the `template` key) with the given
       `data` hash.
    4. Compare the results of your rendering against the `expected` value;
       any differences should be reported, along with any useful debugging
       information.
       * Of note, the `desc` key contains a rough one-line description of
         the behavior being tested – this is most useful in conjunction with
         the file name and test `name`.
