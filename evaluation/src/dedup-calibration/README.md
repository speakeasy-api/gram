# Dedup-cosine calibration probe

Probes the right pre-write dedup threshold for `openai/text-embedding-3-small`
on the Assistant Memory RFC. Embeds a labelled fixture once via OpenRouter,
then sweeps four similarity variants × nine thresholds × three buckets in
memory.

## Why

The RFC currently picks `cosine ≥ 0.97` as the pre-write dedup threshold
without measuring the model's actual similarity distribution. Picking a
threshold without calibration lands you in one of two failure modes:

- **Too tight** (e.g. 0.97 when paraphrase pairs sit at 0.85–0.95): the
  store fills with rephrased twins of the same fact.
- **Too loose** (e.g. 0.85 when distinct facts about the same topic sit
  at 0.85+): `remember()` silently drops genuinely new memories.

This probe measures both bands directly so the threshold choice is data-
driven.

## What it tests

By default benches **seven embedding models** in parallel and reports a
cross-model summary plus detailed tables for the winner. Override with
`EMBED_MODELS=foo/bar,baz/qux`.

Default model list (in `calibrate.ts`):

- `openai/text-embedding-3-small` — RFC's current pick (control)
- `openai/text-embedding-3-large` — same family, 3072 dims
- `qwen/qwen3-embedding-8b` — recent MTEB English leader
- `qwen/qwen3-embedding-4b` — faster Qwen3 alternative
- `google/gemini-embedding-001` — strong factoid retrieval
- `mistralai/mistral-embed-2312` — popular hosted option
- `baai/bge-large-en-v1.5` — open-source MTEB strong performer

Three labelled buckets in `fixture.ts`:

| bucket             |   n | meaning                                           |
| ------------------ | --: | ------------------------------------------------- |
| `unrelated`        |  81 | two unrelated facts → sets noise floor            |
| `true-duplicate`   |  50 | same fact, paraphrased → MUST be deduped          |
| `related-distinct` |  30 | same topic, different value → MUST NOT be deduped |

Four similarity variants:

1. **`raw`** — cosine on the raw text embedding (RFC's current choice).
2. **`norm`** — cosine on lowercased + punctuation-stripped text.
3. **`strip`** — cosine after stripping the leading "The user …" anchor.
4. **`composite`** — `max(raw cosine, Jaccard 3-gram)` (memberry's pattern).

Each variant is evaluated at thresholds `{0.99, 0.97, 0.95, 0.92, 0.90,
0.85, 0.80, 0.75, 0.70}`.

## Running

```bash
cd gram/evaluation
pnpm install                        # if you haven't already
export OPENROUTER_API_KEY=sk-or-...
pnpm calibrate-dedup
```

Or drop the key in a `.env` file alongside `package.json` and let Node
load it via `--env-file`:

```bash
echo "OPENROUTER_API_KEY=sk-or-..." > .env
pnpm calibrate-dedup
```

Optional: dump raw per-pair scores to disk for plotting elsewhere.

```bash
CALIBRATION_OUTPUT_PATH=./dedup-scores.json pnpm calibrate-dedup
```

## Cost

Three batched embed calls per model (raw + normalized + subject-stripped)
× ~265 unique strings each. ~30 tok/string. Across all seven default
models ≈ 55K tok total at $0.01–$0.13/1M depending on model — **under
$0.05 for the full bench**. Sub-cent for any single model.

Wall time: ~10–20 seconds (bounded by the slowest model; all run in
parallel).

## Reading the output

Two tables print to stdout:

### Per-bucket score distribution

```
variant    | bucket            |   n |   min |    p5 |   p50 |   p95 |   max
-----------|-------------------|----:|------:|------:|------:|------:|------:
raw        | unrelated         |  80 |  ...  |  ...  |  ...  |  ...  |  ...
raw        | related-distinct  |  30 |  ...  |  ...  |  ...  |  ...  |  ...
raw        | true-duplicate    |  50 |  ...  |  ...  |  ...  |  ...  |  ...
```

What you want: `true-duplicate p5` is **above** `unrelated p95` AND
`related-distinct p95`. That gap is the threshold band.

If the bands overlap (e.g. `true-duplicate p5 = 0.78` but
`related-distinct p95 = 0.83`), raw cosine alone cannot dedup safely on
this embedder — check the `composite` row, which adds a lexical fallback.

### Sweep table

```
variant    |  thr | true-dup | unrelated | related-distinct | separation
-----------|-----:|---------:|----------:|-----------------:|----------:
raw        | 0.95 |   46.0%  |     0.0%  |             6.7% |     39.3%
raw        | 0.92 |   72.0%  |     0.0%  |            13.3% |     58.7%
...
```

Pick the row maximizing `separation = true_dup_rate − max(unrelated_rate,
related_distinct_rate)`. That's your knee.

### Danger / miss examples

The script also prints the 10 highest-cosine `related-distinct` pairs
(the ones most likely to get falsely deduped) and the 10 lowest-cosine
`true-duplicate` pairs (the ones most likely to slip through). Inspect
these — they sanity-check the fixture and surface real edge cases.

## Decision rules

After running:

- **Clean separation (true-dup p5 > related-distinct p95):** use raw
  cosine at the knee threshold. Done.
- **Bands overlap by < 0.05 cosine:** use composite (raw OR Jaccard) at
  the knee. Cheaper than a model swap.
- **Bands overlap by > 0.05 cosine:** the model is too coarse for this
  task; bump dimensionality (`text-embedding-3-large` is 3072 dims) or
  add a contradiction-detection pass before write. Don't ship a single-
  threshold dedup on this embedder.

## Extending

- **Larger fixture:** `fixture.ts` is hand-curated. Real assistant
  memories may have different distribution; pull a sample of saved
  memories from a test scope and re-run.
- **Different model:** change `EMBED_MODEL` in `calibrate.ts`. RFC
  notes the model is locked at MVP, but if the bands overlap badly,
  this is the cheapest way to evaluate `3-large` or alternatives.
- **Fact-specific subject stripping:** the current `stripSubject` only
  handles "The user …" / "User …". Production memories may use other
  framings (e.g. "User's …", "User has …"). Extend the regex if needed.

## Artifact

Once a threshold is chosen, document it as a finding in the RFC PR
description or a separate doc. memberry's
`docs/findings/2026-04-22-tau-n-calibration-against-bge-m3.md` is a
template for what that durable record looks like.
