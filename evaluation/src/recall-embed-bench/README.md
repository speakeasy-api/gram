# Recall-side embedding cost + perf bench

Measures the recall-path cost and latency of the chosen embedding model
(Qwen3-Embedding-8B), and gives a hypothetical-migration cost picture
against alternatives.

## Why

Every `recall(query)` triggers one embedding call to OpenRouter before
the HNSW lookup. The query embedding model **must match the write-side
model** — otherwise the vectors live in different spaces and cosine is
meaningless. So this bench is **not** a free per-recall model choice. The
non-Qwen-8B cells answer:

> If we re-embedded every existing row to migrate to a cheaper model,
> what would we save on recall costs going forward?

Weighed against a fresh dedup re-calibration cost.

## What it tests

- **5 candidate embedding models** × **3 query-shape buckets** ×
  **N queries**. 10 queries per bucket, 30 per model, 150 calls total.
- Per cell: p50 / p95 / p99 latency, mean input tokens, cost per 1M
  recalls at OpenRouter list pricing.
- Single-call mode (no batching) — production embeds one query per
  recall.

Query shape buckets:

| bucket   |   n | shape                               |
| -------- | --: | ----------------------------------- |
| `short`  |  10 | 3–10 token keyword phrasings        |
| `medium` |  10 | 20–40 token full-question phrasings |
| `long`   |  10 | 80–150 token multi-clause queries   |

The shape mix matters because embedding latency and cost both scale with
input token count. Production traffic skews short, but the long bucket
sets the upper bound an agent could hit.

## Running

```bash
cd gram/evaluation
export OPENROUTER_API_KEY=sk-or-...
pnpm bench-recall-embed
```

Or via `.env` at `evaluation/.env`.

## Knobs

- `EMBED_MODELS=qwen/qwen3-embedding-8b,qwen/qwen3-embedding-4b
pnpm bench-recall-embed` — restrict the model set.
- `PER_MODEL_CONCURRENCY=4 pnpm bench-recall-embed` — lower in-flight
  per model (default 8). Some providers rate-limit aggressively.
- `WARMUP_CALLS=2 pnpm bench-recall-embed` — discard first N per model
  to dodge cold-route bias.
- `MONTHLY_RECALLS=10000000 pnpm bench-recall-embed` — projects
  cost-per-month at this recall volume.
- `EMBED_PRICING_JSON='{"qwen/qwen3-embedding-8b":0.07}' pnpm bench-recall-embed`
  — override pricing if list rates shifted.
- `RECALL_EMBED_OUTPUT_PATH=./recall-embed.json pnpm bench-recall-embed`
  — dump every call (latency, tokens, dims) for offline analysis.

## Cost

150 calls × ~50 tokens/call avg = ~7,500 tokens × 5 models = ~38k tokens
total. At even the most expensive embedder (~$0.13/1M), pennies. Wall:
under a minute parallel.

## Reading the output

### Cross-model summary

```
model                                |   dim |   p50 |   p95 |   p99 | mean tok |  $/1M recalls | err
-------------------------------------+-------+-------+-------+-------+----------+---------------+-----
openai/text-embedding-3-small        |  1536 |    73 |   145 |   210 |     34.2 |        $0.001 |   0
qwen/qwen3-embedding-4b              |  2560 |   120 |   220 |   320 |     38.7 |        $0.002 |   0
qwen/qwen3-embedding-8b              |  4096 |   180 |   340 |   480 |     38.7 |        $0.003 |   0
...
```

Sorted ascending by cost-per-1M-recalls. The **Qwen3-8B row is the one
we ship** — others are migration references. Don't pick the cheapest
without re-validating dedup quality.

### Per-shape breakdown

```
--- qwen/qwen3-embedding-8b (per query shape) ---
bucket     |    n |   p50 |   p95 |   p99 | mean tok |     $/1M
-----------+------+-------+-------+-------+----------+----------
short      |   10 |   140 |   180 |   180 |     5.2  |   $0.000
medium     |   10 |   180 |   240 |   240 |    27.3  |   $0.002
long       |   10 |   240 |   380 |   380 |    72.6  |   $0.005
all        |   30 |   180 |   340 |   480 |    35.0  |   $0.002
```

Latency scales with input tokens. If `long` p95 is past your recall-path
SLO budget, consider a short-circuit on long queries (truncate the
agent-generated query before embed) — the agent's verbosity is the lever.

### Monthly projection

```
=== Monthly cost projection at 10,000,000 recalls/month ===
model                                |    $/month
-------------------------------------+------------
openai/text-embedding-3-small        |       $14
qwen/qwen3-embedding-4b              |       $19
qwen/qwen3-embedding-8b              |       $30
openai/text-embedding-3-large        |       $193
```

Override the volume with `MONTHLY_RECALLS=50000000`.

## Decision rules

- **Qwen3-8B p95 < 500 ms across all shapes:** ship as-is, recall-path
  budget is fine.
- **Qwen3-8B p95 > 1 s on `medium`/`long`:** consider truncating
  agent-generated queries before embed, or batching multiple recalls
  per agent turn.
- **A cheaper model is 3×+ cheaper at acceptable Qwen3-8B p95 parity:**
  flag for migration discussion. Migration cost = re-embed every row
  (one batched call per existing memory) + re-calibrate dedup threshold
  on the new model's distribution. Probably only worth it if the chosen
  model has a clear path to >50% cost reduction at scale.
- **Otherwise:** confirm Qwen3-8B and document the baseline.

## Caveats

- **Pricing is OR list as of late-2025.** Verify before quoting; OR
  rates fluctuate. Override via `EMBED_PRICING_JSON`.
- **Single-call latency only.** Batched embedding (multiple queries in
  one call) is faster per-token but the recall path doesn't batch.
- **`baai/bge-large-en-v1.5` pricing depends on the OR provider routing
  it.** Open-weight models with multiple providers can have wide
  variance — re-bench at procurement time.
- **Synthetic queries.** Real agent-generated recalls may have
  different distribution. Replay a sample from a real `recall` log once
  the feature has telemetry.
- **Dedup quality on alternate models is from `dedup-calibration`, not
  this bench.** Don't migrate on cost alone; cross-reference the
  Calibration toggle in the Notion RFC for separation scores.
