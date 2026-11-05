# UI fuzz: adversarial screen states judged by Jev

Every static dashboard route is visited with one of its API responses
rewritten, the accessibility tree of the main region is snapshotted, and Jev
(TypeSafe System One, `typesafe/jev-1.13` via OpenRouter, the judge the
launcher and the prompt-injection prefilter already use) answers two
questions about it:

- `state`, a choice: `loaded`, `empty`, `loading`, `error` or `broken`.
- `broken`, a probability: is any rendering defect visible.

Each mutator says which states a correct UI may land in. A confident answer
outside that set, or a `broken` probability at or above the threshold, fails
the case. Everything is written to `e2e/.ui-fuzz/<run>/`, passing cases
included, with the snapshot that was judged, so two runs can be diffed.

## Running

```
mise run zero:openrouter             # once; keeps the key in mise.local.toml
mise run test:ui-fuzz                # wakes the stack, records, fuzzes, summarizes
mise run test:ui-fuzz --skip-record  # reuse the latest recording
mise run test:ui-fuzz -- --grep mcp  # one route's cases
node --test e2e/ui-fuzz/*.test.ts    # the pure parts, free
```

In CI it runs weeknights and on demand (`.github/workflows/ui-fuzz.yml`),
never per PR. The workflow needs an `OPENROUTER_CI_KEY` repository secret with
a spend cap set on the OpenRouter side.

## How a run works

1. **Catalogue.** `catalogue.ts` reads `client/dashboard/src/routes.tsx` and
   lists every route. Phase 1 takes the ones reachable with only an org and
   a project slug: no `:param`, no splat, not unauthenticated, external or a
   legacy redirect. Nobody keeps a page list by hand.
2. **Record.** `tests/ui-fuzz-record.spec.ts` visits each of those routes
   unmutated and writes the `GET /rpc/*` calls it made to
   `e2e/.ui-fuzz/deps.json`. Calls seen on more than half the routes are the
   app frame (session, project, flags) and are excluded: breaking those
   breaks every screen the same way. No judge is called in this step.
3. **Fuzz.** `tests/ui-fuzz.spec.ts` generates, per route, one baseline case
   and one case per remaining dependency × mutator. The baseline records are
   the calibration set.
4. **Summarize.** `summarize.ts` merges the per-worker files into
   `cases.jsonl` and `summary.json` and prints the failures.

## Mutators

`mutations.ts` is shape-agnostic: it rewrites the recorded real response, so
no endpoint knowledge is needed.

| Mutator        | What it does                                     | Allowed states    |
| -------------- | ------------------------------------------------ | ----------------- |
| `empty-arrays` | every array becomes `[]`                         | `empty`, `loaded` |
| `long-names`   | every label-like string becomes 5,000 characters | `loaded`, `empty` |
| `server-error` | 500 with a goa error envelope                    | `error`           |
| `forbidden`    | 403 with a goa error envelope                    | `error`           |

A mutator that finds nothing to change in a payload skips the case with a
reason instead of judging an unmutated screen.

## Layout

- `jev.ts`: the judgement client. Mirrors
  `server/internal/thirdparty/typesafe/client.go`.
- `questions.ts`: the state document and the two questions. The wording is the
  product; when probabilities do not separate, change the criteria first.
- `catalogue.ts`: the route table reader.
- `mutations.ts`: the mutators.
- `record.ts`: the verdict rule and the per-case record.
- `run.ts`: run id, directories, the recording file, frame detection.
- `fixtures.ts`: one dev-idp login per worker, shared through storage state.

## Phases

1. Static index routes, generic mutators. This.
2. Parameterised routes (`:toolsetSlug`, `:chatId`, ...) by crawling the
   links the parent list page renders, so seed data supplies the instances.
3. Dialogs, sheets and wizard steps, which need an interaction script per
   state.

Run the golden pass and calibrate before any phase gates.

## Thresholds are not calibrated

`CHOICE_CONFIDENCE` and `BROKEN_THRESHOLD` in `record.ts` are starting points
borrowed from the prompt-injection prefilter. Before trusting a failure, read
the baseline records of a full run: they must be clean, and their probabilities
set the thresholds. Re-derive them on every model bump; the model id is pinned
in `jev.ts` for that reason.
