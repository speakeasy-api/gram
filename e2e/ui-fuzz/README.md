# UI fuzz: adversarial screen states judged by Jev

Each case rewrites one API response a dashboard screen depends on, snapshots
the accessibility tree of the main region, and asks Jev (TypeSafe System One,
`typesafe/jev-1.13` via OpenRouter, the same judge the launcher and the
prompt-injection prefilter use) two questions:

- `state`, a choice: `loaded`, `empty`, `loading`, `error` or `broken`.
- `broken`, a probability: is any rendering defect visible.

The mutation says which states a correct UI may land in. A confident answer
outside that set, or a `broken` probability at or above the threshold, fails
the case. Everything is written to `e2e/.ui-fuzz/<run>/cases.jsonl` with the
snapshot it judged, so two runs can be diffed.

## Running

```
mise run zero:openrouter      # once; keeps the key in mise.local.toml
mise run test:ui-fuzz         # wakes the stack, runs every case
mise run test:ui-fuzz --grep empty
node --test e2e/ui-fuzz/*.test.ts   # the pure parts, free
```

In CI it runs weeknights and on demand (`.github/workflows/ui-fuzz.yml`),
never per PR. The workflow needs an `OPENROUTER_CI_KEY` repository secret with
a spend cap set on the OpenRouter side.

## Layout

- `jev.ts`: the judgement client. Mirrors
  `server/internal/thirdparty/typesafe/client.go`.
- `questions.ts`: the state document and the two questions. The wording is the
  product; when probabilities do not separate, change the criteria first.
- `mutations.ts`: per-screen mutation tables. Pure transforms over the
  response body, so they are unit tested.
- `record.ts`: the verdict rule and the per-case record.
- `../tests/ui-fuzz.spec.ts`: logs in, installs a mutation with `page.route`,
  snapshots, asks, records.

## Adding a screen

Add an entry to `screens` in the spec with the route, a one-sentence purpose,
and a mutation table. A mutation names the rpc path it intercepts, the states
it allows, and `apply(original)`, which returns a response or a skip reason.

## Thresholds are not calibrated

`CHOICE_CONFIDENCE` and `BROKEN_THRESHOLD` in `record.ts` are starting points
borrowed from the prompt-injection prefilter. Before trusting a failure, build
a golden set: run every screen unmutated, require zero findings, and set the
thresholds from those probabilities. Re-derive them on every model bump; the
model id is pinned in `jev.ts` for that reason.
