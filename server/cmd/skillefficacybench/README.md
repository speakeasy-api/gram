# skillefficacybench

Runs the production skill-efficacy prompt and schema against a synthetic labeled
corpus. It uses the real OpenRouter completion client with the production usage
source, internal key type, zero temperature, and structured verdict parser.

```sh
export OPENROUTER_DEV_KEY=sk-or-...
go run ./server/cmd/skillefficacybench
go run ./server/cmd/skillefficacybench -runs 3 -models google/gemini-3.1-flash-lite
go run ./server/cmd/skillefficacybench -baseline server/cmd/skillefficacybench/baseline.json
```

The committed set contains only generic synthetic skills and transcripts. Raw
results contain case IDs and measurements, not skill or transcript bodies, and
are ignored by git.

## Rubric

- `0.00`: No help. The skill was irrelevant, ignored, misapplied, or made the outcome worse.
- `0.25`: Slight help. Applicable guidance appeared but had little demonstrated effect.
- `0.50`: Moderate help. Partial adherence produced a useful effect with material omissions or corrections.
- `0.75`: Strong help. The skill was mostly followed and clearly reduced wrong turns or rework.
- `1.00`: Decisive help. The skill directly drove success or prevented substantial rework.

Each case declares an inclusive expected score band. A case agrees when the
median successful score is inside that band; a case with no successful verdict
disagrees. The current beta exit gate is **80% case agreement** for the
production model and prompt version. Individual errors remain visible in the
run-agreement and error metrics. The command exits nonzero below the gate.

`-baseline` compares mean scores with a prior single-model raw result file and
prints per-case drift, including across model changes. Any production model,
prompt, schema, or rubric change must run this bench before rollout. Prompt or rubric changes must also increment
`efficacy.JudgePromptVersion`; the corpus version check prevents a stale set from
running silently.

## Current result

The initial `v2` gate run on 2026-07-22 used the production
`google/gemini-3.1-flash-lite` model with three runs per case. It reached 90%
case agreement and 86.7% run agreement with no errors, passing the 80% beta gate.
