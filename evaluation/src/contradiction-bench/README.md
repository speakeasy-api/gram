# Contradiction-detection model bench

Picks the cheapest LLM for the `contradicts(A, B)` pre-write classifier
that the three-branch dedup logic depends on. Runs every candidate model
against a 190-pair labelled fixture, sweeps confidence thresholds,
reports F1 on the `contradicts: true` label.

## Why

In the proposed three-branch write logic for assistant memory:

- `cosine ≥ 0.92` → Skip (already proven by `dedup-calibration`).
- `cosine ∈ [0.65, 0.92)` → call this model to ask "does B contradict A?"
  - if yes → Supersede (mark old, link new).
  - else → Create.
- `cosine < 0.65` → Create.

The contradiction model fires on ~10–15% of writes. It needs to be:
fast (in the user-perceived write path), accurate (false positives
silently lose data; false negatives leave both versions coexisting),
and cheap (it runs on every borderline pair).

## What it tests

Four labelled buckets:

| bucket          |   n | gold `contradicts` | description                                 |
| --------------- | --: | ------------------ | ------------------------------------------- |
| `contradicting` |  55 | true               | same dimension, different value (Supersede) |
| `refining`      |  40 | false              | B is more specific form of A (both true)    |
| `extending`     |  45 | false              | B is a different dimension (both true)      |
| `unrelated`     |  50 | false              | different topics                            |

The non-contradicting buckets are split so per-model false-positive
analysis surfaces _which kind_ of mistake a model makes. A model that
gets `extending` wrong (overlapping subjects fool it) is different from
one that gets `refining` wrong (overlapping dimensions fool it).

## Default candidate models

Hard-coded in `bench.ts`:

- `anthropic/claude-haiku-4-5` — fast Anthropic
- `~openai/gpt-mini-latest` — OpenAI fast tier (alias)
- `~google/gemini-flash-latest` — Gemini Flash (alias)
- `qwen/qwen3.6-flash` — Qwen Flash
- `qwen/qwen3.6-35b-a3b` — Qwen sparse (memberry's lineage)
- `deepseek/deepseek-v4-flash` — cheap DS
- `mistralai/mistral-medium-3-5` — Mistral mid-tier

Override with `MODELS=anthropic/claude-haiku-4-5,openai/gpt-5.5 pnpm bench-contradiction`.

If a default model ID is wrong (OR catalog moves), the bench logs the
failure and continues with the others. Pull from
`https://openrouter.ai/api/frontend/models` for current IDs.

## Running

```bash
cd gram/evaluation
export OPENROUTER_API_KEY=sk-or-...
pnpm bench-contradiction
```

Or via `.env` at `evaluation/.env`. Same convention as
`dedup-calibration`.

`VERBOSE=1` prints detailed reports for every model. By default only
the top model gets the detailed view.

`PER_MODEL_CONCURRENCY=4` lowers in-flight calls per model (default 8;
some providers rate-limit aggressively).

`CONTRADICTION_OUTPUT_PATH=./contradiction-results.json` dumps every
call (including raw output, latency, tokens) for offline analysis.

## Cost

Each call is ~400 in + 30 out ≈ 430 tokens. 190 pairs × 7 models ≈
1,330 calls ≈ 600k tokens. At OR avg pricing ($1–3/1M for fast tier):
**~$0.50–$1 total**. Wall: 1–3 minutes parallel.

## Reading the output

### Cross-model summary

```
model                                    | best_thr | precision | recall |  F1   | p50 ms | p95 ms | parse_fail | tokens
-----------------------------------------|---------:|----------:|-------:|------:|-------:|-------:|-----------:|------:
anthropic/claude-haiku-4-5               |     0.75 |     94.3% |  91.7% | 0.930 |    320 |    580 |          0 | 79324
...
cosine baseline (illustrative) [0.65,0.92) |    — |     35.0% |  62.0% | 0.451 |     ~5 |    ~10 |          — |     —
```

Sorted by F1. The cosine-baseline rows are illustrative — they show the
band-only classification you'd get without the LLM, to make the value
of the LLM call concrete. **Do not ship the cosine baseline; it's there
to motivate why we're paying for the LLM in the first place.**

### Confusion matrix (top model)

```
                  | predicted: contradicts | predicted: not |
gold: contradicts |                     55 |              5 |
gold: not         |                      4 |            126 |
```

- truePos: model + Supersede = correct
- falsePos: model + Supersede when it shouldn't have → **silently loses
  data** (the predecessor gets marked superseded for no reason)
- trueNeg: model + Create = correct
- falseNeg: model said "not contradiction" but it was → **both versions
  coexist** (recall returns whichever scored higher)

In production false-positives are worse than false-negatives:
false-negatives just degrade slightly to current RFC behavior;
false-positives erase user data.

### Per-bucket false-positive breakdown

Tells you _where_ the model is failing:

```
refining   → 2/40 false-positive
extending  → 1/40 false-positive
unrelated  → 1/50 false-positive
```

A model that mostly errs on `refining` is conflating "more specific"
with "different value." A model that errs on `extending` is being
confused by shared subjects. Different prompt fixes, if you ever want
to per-model tune.

## Decision rules

- **F1 ≥ 0.85, p95 < 800 ms, sub-cent per call:** ship it.
- **F1 < 0.85 across the board:** prompt needs work; try
  decision-cascade phrasing or add per-model exemplars.
- **Multiple winners:** prefer lower p95 latency over higher F1, since
  false-negatives just leave us at current-RFC behavior.
- **High false-positive rate on `refining`:** strengthen the
  "refines = both true" example in the prompt.

## Decision rules at threshold

The bench sweeps confidence thresholds (0.0 to 0.95) per model. The
"best" cell is the highest F1, but two thresholds on a single model can
have similar F1 with different precision/recall tradeoffs. If
production wants to err toward false-negatives (preserve data), pick a
_higher_ threshold than the F1-maximum. The full sweep is in the JSON
dump (`CONTRADICTION_OUTPUT_PATH=...`).

## Caveats

- **Fixture is hand-curated and synthetic.** Production agent writes
  may have different shapes. Re-run with a real-traffic sample once the
  feature is in beta.
- **Single canonical prompt across models** — no per-model tuning. If a
  model F1s low here, it's not yet evidence that the model is
  fundamentally bad at the task; try its preferred prompt patterns
  first.
- **`temperature: 0` everywhere.** Some models (notably Qwen-2507
  variants, when present in the slate) leak `<think>` tokens at low
  temperature. The parser strips ```code fences but won't recover from
  prefix reasoning leaks. Check parse-failure rates per model.
- **Cosine baseline uses the dedup-probe winner** (`qwen/qwen3-embedding-8b`)
  — its illustrative numbers are for that embedder specifically. Stays
  here as a sanity check that the LLM is doing real work.

## Artifact

Once a model is picked, document it as a finding alongside the dedup
calibration. The Notion RFC's "Calibration methodology" toggle is the
template; this probe should produce a sibling toggle covering: model
choice, threshold, latency budget, fallback (when LLM call fails →
Create), and the false-positive / false-negative tradeoff.
