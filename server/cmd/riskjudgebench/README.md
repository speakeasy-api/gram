# riskjudgebench

Benchmarks OpenRouter models for **prompt-based ("LLM-judge") risk policy
evaluation** — the call in
[`server/internal/riskjudge/judge.go`](../../internal/riskjudge/judge.go).

It drives the **real production `openrouter.ChatClient`**
(`NewUnifiedClient` → `GetObjectCompletion`), so every model runs under
prod-equivalent conditions:

- reasoning disabled (`Effort:"none"`), as the object-completion path forces,
- the production model **allowlist** + `ResolveModel` fallback,
- the same guardian-policy HTTP transport,
- the identical `ObjectCompletionRequest` shape `judge.call()` builds (system
  prompt, strict JSON schema, temperature, `UsageSource`).

One knob differs from prod by default: the per-call timeout is `30s` here vs the
production `judgeTimeout` of `10s`, to avoid spurious timeouts when benchmarking
slower models. Pass `-timeout 10s` for exact production timeout parity.

The only stubs are org-scoped concerns that don't affect model quality/latency:
a `Provisioner` that returns the dev key instead of a DB-backed per-org key, and
nil capture/usage/title/telemetry strategies (all nil-guarded in the client).

## Run it

```sh
export OPENROUTER_DEV_KEY=sk-or-...        # or OPENROUTER_API_KEY; mise.local.toml already sets it
cd /path/to/gram

go run ./server/cmd/riskjudgebench                       # default models, 1 run/case, prod schema
go run ./server/cmd/riskjudgebench -runs 3               # 3 runs/case for latency + stability
go run ./server/cmd/riskjudgebench -models anthropic/claude-sonnet-4.6,google/gemini-3.1-flash-lite
go run ./server/cmd/riskjudgebench -h
```

Flags: `-models` (must be allowlisted), `-cases`, `-runs`, `-concurrency`,
`-temperature`, `-timeout`, `-org`, `-out`.

`precision` guards against false alarms (over-flagging benign messages);
`recall` guards against missed violations. Ranked by F1, tie-broken by p50
latency. `avgTok` is the mean total tokens per call (cost proxy — real cost is
tracked out-of-band in prod via the usage-tracking strategy, which is stubbed
here).

Every run also prints a **confidence-threshold sweep**: precision/recall/F1 if a
flag were gated on `matched && confidence >= tau` instead of `matched` alone
(`tau=0.00` reproduces the main table and current prod). It reuses the
already-collected calls, so it costs nothing extra. Use it to see whether a
model's mistakes are suppressible (low-confidence) or baked in (confident).

## Schema constraints

`judge.go` used to constrain `confidence` with `minimum`/`maximum` and
`rationale` with `maxLength`. **Anthropic routes reject these**
(`For 'number' type, properties maximum, minimum are not supported`, via Amazon
Bedrock), so every Anthropic model returned a 400 and the judge fail-opened.
That bench finding has since been applied: **`judge.go` drops those constraints
and enforces the bounds in code** (confidence clamped via `max(0,min(1,…))`,
rationale truncated to 500 chars). The bench mirrors that schema, so it works for
all routes.

## Findings (40-case dataset, real client, temp 0, `-runs 3` = 120 evals/model)

| model                                        | acc   | prec  | rec   | p50 ms   | p95 ms | err | avgTok | notes                                                              |
| -------------------------------------------- | ----- | ----- | ----- | -------- | ------ | --- | ------ | ------------------------------------------------------------------ |
| **google/gemini-3.1-flash-lite** _(default)_ | 0.941 | 0.903 | 0.982 | ~1094    | ~1822  | 1   | 346    | best raw F1; fast; cheapest tier; FPs are conf=1.00 (untunable)    |
| anthropic/claude-sonnet-4.6                  | 0.933 | 0.902 | 0.965 | ~2046    | ~3573  | 0   | 466    | **best of all once gated at conf≥0.90 → F1 0.947**; pricier/slower |
| google/gemini-2.5-flash                      | 0.917 | 0.912 | 0.912 | **~644** | ~1388  | 0   | 262    | fastest by far; balanced; untunable via confidence                 |
| openai/gpt-5.4-nano                          | 0.904 | 0.823 | 1.000 | ~1684    | ~2859  | 6   | 304    | perfect recall but over-flags; **refuses** security content        |
| deepseek/deepseek-v4-flash                   | 0.900 | 0.869 | 0.930 | ~3248    | ~13552 | 0   | 249    | slow p95; over-flags                                               |
| anthropic/claude-haiku-4.5 _(prev. default)_ | 0.900 | 0.895 | 0.895 | ~1489    | ~1966  | 0   | 470    | genuine recall gap (prompt-injection, bulk-export); gating →0.899  |
| openai/gpt-5.4-mini                          | 0.895 | 0.831 | 0.961 | ~1317    | ~1867  | 6   | 299    | over-flags; same refusal behavior                                  |
| google/gemini-3.5-flash                      | —     | —     | —     | —        | —      | 120 | —      | 400: _"Reasoning is mandatory for this endpoint"_ (unusable)       |
| mistralai/mistral-medium-3.1                 | —     | —     | —     | —        | —      | 120 | —      | 404: org data-policy restriction (unusable)                        |

Confidence-threshold sweep highlights (`positive = matched && conf>=tau`):

| model                        | tau=0.00 F1 | best F1   | at tau | what it means                              |
| ---------------------------- | ----------- | --------- | ------ | ------------------------------------------ |
| anthropic/claude-sonnet-4.6  | 0.932       | **0.947** | 0.90   | mistakes are low-confidence → gating helps |
| openai/gpt-5.4-nano          | 0.903       | 0.926     | 0.90   | over-flagging partly suppressible          |
| google/gemini-3.1-flash-lite | 0.941       | 0.941     | —      | flat: FPs at conf=1.00, nothing to gate    |
| google/gemini-2.5-flash      | 0.912       | 0.912     | —      | flat then collapses; confident errors      |
| anthropic/claude-haiku-4.5   | 0.895       | 0.899     | 0.90   | barely moves; recall gap is structural     |

Takeaways:

- **`gemini-3.1-flash-lite` wins on raw F1** and is fast + cheapest — now the
  default judge model (`riskjudge.defaultJudgeModel`). Its few false alarms are at
  `conf=1.00` — confidently wrong, so a confidence gate can't tune them out, an
  acceptable FP floor for a high-volume classifier.
- **`claude-sonnet-4.6` is the best ceiling**: gated at `conf≥0.90` it reaches
  F1 **0.947** (FP 6→3, no recall loss), edging past gemini-lite — at ~2× the
  latency and a higher price tier.
- **`gemini-2.5-flash` is the latency pick** — ~644ms p50 (~2.5× faster than the
  previous default), 0 errors, balanced precision/recall.
- **OpenAI minis refuse** security content (6 errors each — a refusal is a
  dropped verdict) and over-flag. Avoid for this use.
- **`claude-haiku-4.5` (the previous default) is mid-pack** even with the schema
  fixed: a real recall gap on prompt-injection/bulk-export that confidence gating
  can't recover. `claude-sonnet-4.6` is the better Anthropic option.

### Recurring hard cases (worth reviewing in `cases.json`)

- `prompt-injection-quoted-analysis-neg` — **false-alarms on 6 of 7 working
  models** (the single biggest precision drag). Likely needs a clearer policy or
  a system-prompt clarification that _quoting/analyzing_ an injection ≠ executing it.
- `high-value-payment-boundary-neg` — false alarms across haiku, sonnet, nano, deepseek.
- `bulk-export-pagination-positive`, `drop-prod-table-truncate-positive` — recall
  misses (genuine violations getting through).
