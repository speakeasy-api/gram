---
name: mise-tasks
description: Rules and best practices for writing and editing mise tasks.
---

## Task naming and location

- Tasks should be placed in `.mise-tasks/` or a subdirectory of it.
- The task path corresponds to the task name
  - Example:
    - `.mise-tasks/start.sh` corresponds to `mise run start`
    - `.mise-tasks/test/app.mts` corresponds to `mise run test:app`
    - `.mise-tasks/agents/worktree/init.sh` corresponds to `mise run agents:worktree:init`
- When creating tasks, run `chmod +x <task>` to make them executable.
- Consult `mise.toml` to understand more about what tools and environment variables are available to support tasks.
- For typescript tasks, the root `package.json` has utilities for writing tasks in TypeScript more effectively.

## Task Configuration for Bash Tasks

- This is how you write [mise](https:/mise.jdx.dev) tasks using bash.
- This is a list of all the relevant configuration options available in shell scripts.
- It is not an exhaustive list. All options can be found [here](https:/mise.jdx.dev/tasks/task-configuration.html).
- Tasks options MUST come right after the initial shebang line
- Each task option must be listed one a separate line and MUST follow the form `#MISE <option>=<value>`.
- All shell scripts MUST start with `#!/usr/bin/env bash` followed by a blank line without exception.
- `gum` is a really good tool for building interactive scripts if these are needed.

<detail open><summary>Example</summary>

```sh
#!/usr/bin/env bash

#MISE description="An example task description"
#MISE dir="{{ config_root }}/example"

set -e
echo "Hello world"
```

</detail>

## Task options

### `description`

- **Type**: `string`

A description of the task. This is used in (among other places) the help output, completions, `mise run` (without arguments), and `mise tasks`.

<detail open><summary>Example</summary>

```sh
#MISE description="Useful description here"
```

</detail>

### `depends`

- **Type**: `string | string[]`

Tasks that must be run before this task. This is a list of task names or aliases. Arguments can be passed to the task, e.g.: `depends = ["build --release"]`. If multiple tasks have the same dependency, that dependency will only be run once. mise will run whatever it can in parallel (up to `--jobs`) through the use of `depends` and related properties.

<detail open><summary>Example</summary>

```sh
#MISE depends=["test","build:go","lint:*"]
```

</detail>

### `env`

- **Type**: `{ [key]: string | int | bool }`

Environment variables specific to this task. These will not be passed to `depends` tasks.

<detail open><summary>Example</summary>

```sh
#MISE env.TEST_ENV_VAR="ABC"
```

</detail>

### `dir`

- **Type**: `string`
- **Default**: <code v-pre>"{{ config_root }}"</code> - the directory containing [mise.toml](mdc:mise.toml).

The directory to run the task from. The most common way this is used is when you want the task to execute in the user's current directory:

<detail open><summary>Example</summary>

```sh
#MISE dir="{{ config_root }}/server"
```

</detail>

### `hide`

- **Type**: `bool`
- **Default**: `false`

Hide the task from help, completion, and other output like `mise tasks`. Useful for deprecated or internal tasks you don't want others to easily see.

<detail open><summary>Example</summary>

```sh
#MISE hide=true
```

</detail>

### `confirm`

- **Type**: `string`

A message to show before running the task. This is useful for tasks that are destructive or take a long time to run. The user will be prompted to confirm before the task is run.

<detail open><summary>Example</summary>

```sh
#MISE confirm="Are you sure you want to cut a release?"
```

</detail>

### `sources`

- **Type**: `string | string[]`

Files or directories that this task uses as input, if this and `outputs` is defined, mise will skip executing tasks where the modification time of the oldest output file is newer than the modification time of the newest source file. This is useful for tasks that are expensive to run and only need to be run when their inputs change.

The task itself will be automatically added as a source, so if you edit the definition that will also cause the task to be run.

This is also used in `mise watch` to know which files/directories to watch.

This can be specified with relative paths to the config file and/or with glob patterns, e.g.: `src/**/*.rs`. Ensure you don't go crazy with adding a ton of files in a glob though—mise has to scan each and every one to check the timestamp.

<detail open><summary>Example</summary>

```sh
#MISE sources=["go.mod", "go.sum", "**/*.{go,sql}"]
```

</detail>

### `outputs`

- **Type**: `string | string[] | { auto = true }`

The counterpart to `sources`, these are the files or directories that the task will create/modify after it executes.

`auto = true` is an alternative to specifying output files manually. In that case, mise will touch an internally tracked file based on the hash of the task definition (stored in `~/.local/state/mise/task-outputs/<hash>` if you're curious). This is useful if you want `mise run` to execute when sources change but don't want to have to manually `touch` a file for `sources` to work.

<detail open><summary>Example</summary>

```sh
#MISE sources=["Cargo.toml", "src/**/*.rs"]
#MISE outputs={ auto = true }
```

</detail>

### `quiet`

- **Type**: `bool`
- **Default**: `false`

Suppress mise's output for the task such as showing the command that is run, e.g.: `[build] $ cargo build`. When this is set, mise won't show any output other than what the script itself outputs. If you'd also like to hide even the output that the task emits, use `silent`.

<detail open><summary>Example</summary>

```sh
#MISE quiet=true
```

</detail>

### `silent`

- **Type**: `bool | "stdout" | "stderr"`
- **Default**: `false`

Suppress all output from the task. If set to `"stdout"` or `"stderr"`, only that stream will be suppressed.

<detail open><summary>Example</summary>

```sh
#MISE silent=true
```

</detail>

## `redactions` <Badge type="warning" text="experimental" />

- **Type**: `string[]`

Redactions are a way to hide sensitive information from the output of tasks. This is useful for things like API keys, passwords, or other sensitive information that you don't want to accidentally leak in logs or other output.

A list of environment variables to redact from the output.

<detail open><summary>Example</summary>

```sh
#MISE redactions=["API_KEY", "PASSWORD"]
```

</detail>

Running the above task will output `echo [redacted]` instead.

You can also specify these as a glob pattern, e.g.: `redactions.env = ["SECRETS_*"]`.

## Task inputs

Tasks may define inputs as flags. It is CRITICAL that you leverage flags and not implicitly rely on environment variables. You can however map environment variables to flags in the task definition. In bash tasks flags look like this:

```sh
#!/usr/bin/env bash

#MISE description="Description of the task goes here"

#USAGE flag "-u --user <user>" # one way to define a flag
#USAGE flag "--user" { # another way to define the same flag
#USAGE   alias "-u"
#USAGE   arg "<user>"
#USAGE }
#USAGE flag "--user" { alias "-u" hide=#true } # hide alias from docs and completions

#USAGE flag "-f --force" global=#true           # global can be set on any subcommand
#USAGE flag "--file <file>" default="file.txt" # default value for flag
#USAGE flag "-v --verbose" count=#true          # instead of true/false $usage_verbose is # of times
                                        # flag was used (e.g. -vvv = 3)

#USAGE flag "--include <pattern>" var=#true            # flag can be repeated (--include a --include b)
#USAGE flag "--include... <pattern>"                   # same as above, ellipsis on flag
#USAGE flag "--include <pattern>..."                   # arg is variadic (--include a b c in one invocation)
#USAGE flag "--include <pattern>" var=#true var_min=1  # at least 1 value required
#USAGE flag "--include <pattern>" var=#true var_max=5  # up to 5 values allowed

#USAGE flag "--color" negate="--no-color" default=#true  # $usage_color=#true by default
                                                  # --no-color will set $usage_color=#false

#USAGE flag "--color" env="MYCLI_COLOR" # flag can be backed by an env var

#USAGE flag "--file <file>"  # args named "<file>" will be completed as files
#USAGE flag "--dir <dir>"    # args named "<dir>" will be completed as directories

#USAGE flag "--file <file>" required_if="--dir"     # if --dir is set, --file must also be set
#USAGE flag "--file <file>" required_unless="--dir" # either --file or --dir must be present
#USAGE flag "--file <file>" overrides="--stdin"     # if --file is set, previous --stdin will be ignored

#USAGE flag "--shell <shell>" {
#USAGE   choices "bash" "zsh" "fish" # <shell> must be one of the choices
#USAGE }

#USAGE flag "--file <file>" long_help="longer help for --help (as oppoosed to -h)"
# this is equivalent to the above but preferred when a lot of space is needed
#USAGE flag "--file <file>" {
#USAGE   long_help r#"longer help for --help (as oppoosed to -h)
#USAGE    even
#USAGE    more
#USAGE    text
#USAGE    "#
#USAGE }
```

## Task Configuration for TypeScript Tasks

Many of the aspects of task configuration for bash tasks also apply to TypeScript tasks. The main things to change over:

- All typescript tasks in `.mise-tasks/**/*.mts` must end in `.mts`
- All typescript tasks must use the shebang: `#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types`

The general layout of a TypeScript task should look like this:

```ts
#!/usr/bin/env -S node --disable-warning=ExperimentalWarning --experimental-strip-types

//MISE description="Description of the task goes here"
//MISE dir="{{ config_root }}"

//USAGE flag "--out-file <file>" required=#true help="Help text for the flag goes here"
//USAGE flag "--color <color>" help="Help text for the flag goes here" { choices "red" "blue" "green" }

import assert from "node:assert";
// You may import zx to do shell-like scripting in TypeScript. It also bundles chalk for colored output.
import { $, chalk } from "zx";
// You may import clack for building interactive prompts in the terminal.
import { isCancel, confirm } from "@clack/prompts";

async function run() {
  const outFilename = process.env["usage_out_file"];
  assert(outFilename, "out file is required");
  const color = process.env["usage_color"] || "blue";
  assert(
    ["red", "blue", "green"].includes(color),
    "color must be one of red, blue, or green",
  );

  // rest of task implementation goes here
}

run();
```
