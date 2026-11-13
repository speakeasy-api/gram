# Platform MCP tool eval

Measures how well an agent picks and calls the Platform MCP tools from a plain
request. Every tool the external endpoint serves has at least one case.

## How it works

- **Cases** (`cases/*.yaml`): a user prompt, the tools that serve it
  (`expect`), reads that are a correct first step towards them (`allow`), and
  argument values the expected call must carry (`args`). Negative cases
  (`expect: []`) pass when the agent calls no Platform tool. Every prompt
  talks about one fictional organization, described in `environment.go`.
- **Surfaces**: each case runs on two surfaces.
  - `direct`: the full catalogue and the full server instructions, minus
    the tools whose schema a model provider rejects (see
    `TestPlatformToolSchemasFitModelProviders`).
  - `progressive`: the search, describe and call tools plus the first-run
    tools, with the small-surface instructions. Search and describe run the
    real discovery code, so ranking is what clients get.
- **Scoring**: the first Platform tool call ends the case, except for the
  orientation reads (`get_platform_context`, `whoami`, `list_projects`,
  `find_mcp`). Those get a stub answer, because the environment already
  answers them. Tools never run, so the eval needs no seeded organization; it uses the
  test database only to compose the live catalogue. A
  case's `allow` reads also end the case: a stub cannot return the opaque
  references and versions that a write needs. The report shows, per model and
  surface:
  - selection: the first call reached an expected tool or an allowed first
    step, or no tool on a negative case;
  - expected tool first: the first call reached an expected tool directly;
  - valid args: the arguments meet the tool's input schema;
  - expected args: the arguments carry the case's `args`;
  - confirmed before preview: a write sent `confirmed: true` on its first
    call, which skips the preview every write tool asks for;
  - turns, prompt tokens per case, cost and errors.

  A misses table lists each case an agent got wrong and what it chose
  instead. Read it to find unclear or overlapping tool descriptions.

## Running

The catalogue is composed live by `composeToolEvalRegistry`. Its services
depend on the test database or on stubs that are never called, so every tool
lists its real description and schema. A tool with a missing dependency would
register a one-line placeholder instead, so the eval would score the wrong
text.

`catalogue.json` is a committed snapshot of what clients are shown: each
surface's server instructions and every tool's description and schema. Its
diff in a pull request shows exactly what an agent will read differently.

CI runs these checks on every PR, with no LLM calls:

- `TestToolEvalCatalogueIsLive`: no eval tool is a placeholder. A new
  dependency gate fails here until the composer supplies it.
- `TestToolEvalCatalogueSnapshotIsCurrent`: `catalogue.json` matches the live
  catalogue. Regenerate it with
  `mise run test:server -run TestToolEvalCatalogueSnapshotIsCurrent ./internal/platformmcp/ -args -update-tooleval-catalogue`.
- `TestToolEvalCasesCoverEveryTool`: every external tool has a case (or a
  reasoned entry in `toolEvalExempt`), every named tool exists, every `args`
  key is in the tool's schema, and no prompt names a tool.
- `TestToolEvalHarnessScoresScriptedAgent`: a scripted agent that always
  picks the expected tool scores 100% on both surfaces. On `progressive` it
  finds each tool by exact name through the real search first.
- `TestPlatformToolSchemasFitModelProviders`: no tool has an input schema a
  model provider rejects. Anthropic rejects a request whose tool schema has
  `oneOf`, `anyOf` or `allOf` at the top level, so a client that lists that
  tool fails every turn. Tools already like this are exempt with a reason
  until fixed, and the direct surface leaves them out.

The judged run calls real models. Set `OPENROUTER_DEV_KEY` first.

```bash
mise run eval:platform-tools --changed-since origin/main
mise run eval:platform-tools --models anthropic/claude-sonnet-5.5,openai/gpt-5.5 --runs 3
mise run eval:platform-tools --surfaces progressive --cases '^risk-' --out report.md
```

`--changed-since` runs only the cases a change could move. It compares
`catalogue.json` and the case files at the merge base with the live
catalogue and the current cases. It then runs the new or edited cases, the
cases that name a changed tool in `expect` or `allow`, and the negative
cases. When the server instructions change, every case runs. With no change,
the run skips. Changes every case reads (the agent prompt and environment,
the server instructions, or the progressive discovery tools) run every case.
`--max-cases` caps the run, keeping the changed cases first, except when the
base has no `catalogue.json`: that first run is the baseline and runs every
case.

A reworded tool can also take requests meant for a neighbouring tool. That
neighbour's cases run only when they name the changed tool in `expect` or
`allow`; run without `--changed-since` after a broad rewording.

The direct surface sends every tool a provider accepts on every turn: about 350 KB of
descriptions and schemas, plus the server instructions. Anthropic models
cache it; other models pay for it on every turn.

### In pull requests

The `platform-tool-eval` job blocks the merge through the CI Gate when a
change makes the agent worse:

- It starts when `catalogue.json`, a case, the baseline or the eval harness
  changes. Every tool or instruction change regenerates `catalogue.json`, so
  no tool change slips past it.
- It runs `--changed-since` against the pull request's base, capped at 60
  cases with the changed cases first.
- The progressive surface is gated: three runs per case, and the job fails
  when a case that passed in most runs of `baseline.json` now fails in most
  runs. A case that was already failing, a case with no baseline entry, or a
  one-run flake does not fail it. Errored runs count as neither.
- The direct surface runs once per case and only reports.
- With prompt caching, a progressive run costs about $0.013 a case and a
  direct run about $0.016, so a full 60-case push costs about $3.40.
- The report goes to the job summary. The report and `baseline.json` updated
  with this run's results go to the `platform-tool-eval` artifact.
- When the branch has no `baseline.json`, the gated run takes every case to
  record one and gates nothing.

It needs the `OPENROUTER_TOOL_EVAL_KEY` repository secret, and skips without
it. A provider outage that errors every case fails the job; re-run it.

### Updating the baseline

`baseline.json` holds, for each case and surface, how many of its runs
passed. Update it when a change improves cases or adds new ones, so the gate
protects them from then on: download `baseline.json` from the pull request's
`platform-tool-eval` artifact and commit it as `tooleval/baseline.json`.
Locally, `--baseline-out` writes the same file. A relative path resolves
against the directory you run mise from, so from the repository root (for a
stacked pull request, pass its base branch instead of `origin/main`, as CI
compares with the pull request base):

```bash
mise run eval:platform-tools --changed-since origin/main --surfaces progressive --runs 3 --gate --baseline-out server/internal/platformmcp/tooleval/baseline.json
```

## Adding or changing a tool

Add a case to the file for its area when you add a tool. When a new tool is
easy to confuse with an existing one, add a case for each that makes the
difference clear. Then regenerate `catalogue.json` and run the checks:

```bash
mise run test:server -run TestToolEvalCatalogueSnapshotIsCurrent ./internal/platformmcp/ -args -update-tooleval-catalogue
mise run test:server -run 'TestToolEval|TestPlatformToolSchemas' ./internal/platformmcp/
```

## Next stage: task completion

This stage scores the first choice only. The next stage runs the chosen tools
against a seeded organization and grades the final outcome, like the team
memory eval. Cases already carry what that needs: `allow` marks the reads a
task takes on the way, and the environment names resources the demo seed can
create.
