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

The committed set contains only generic synthetic skills and transcripts.
Sanitized results contain case and pair IDs, compatibility metadata, stable error
classes, measurements, counts, accuracies, and grading booleans. They never contain
skill or transcript bodies, expected labels, recommendation enum values, evidence
indices, notes, aliases, or raw completions. Result files are ignored by git.

## Rubric and quorum

- `0.00`: no help, misapplication, or a worse outcome.
- `0.25`: slight help with little demonstrated effect.
- `0.50`: useful partial adherence with material omissions or corrections.
- `0.75`: strong help that clearly reduced wrong turns or rework.
- `1.00`: decisive help that drove success or prevented substantial rework.

Each score case declares an inclusive score band. Agreement requires a strict
majority of all attempted runs to return valid scores and the successful-run
median to fall in that band. The five A/B pairs are recommendation-only, leaving
exactly ten independent score cases. Invalid verdicts still contribute returned
usage. The score case-agreement gate is **80%**.

Recommendation agreement also requires a strict majority of all attempted runs.
The expanded corpus has eight positive and twelve zero-recommendation cases.
Positive, zero/suppression, and complete-pair case agreement each have an
independent **80%** gate. High-confidence persistence precision has a **100%**
gate; persistence recall is reported separately. Extra recommendations fail the
exact count and reduce persistence precision when they are high confidence.

## v11 structured recommendation methodology

Recommendation schema version 3 grades a deterministic taxonomy and transcript
grounding instead of free-text note entailment. Each positive label has reviewed
acceptable outcomes, one acceptable issue type, one acceptable change type, a
persistence expectation, allowed transcript evidence indices, and required
evidence groups. Every emitted evidence index must be a strictly increasing,
unique member of the allowed set. Each required group is an OR-list of
alternatives and must contribute at least one cited index; groups compose with
AND. Exact agreement requires the count, outcome, confidence/persistence, issue
type, change type, allowed evidence subset, and all required groups to match.
The harness reports issue, change, and evidence booleans and aggregate
accuracies/counts without exposing their values.

Recommendation notes are not graded. They remain private model output and can be
reviewed separately from this deterministic benchmark. Paraphrase, polarity, and
negation in a note therefore cannot change a structured grade.

At load time, strict JSON decoding requires exactly one value. The loader validates
all required case metadata, reviewed outcome sets, one positive expectation at
most, known issue/change labels, sorted unique evidence labels, evidence membership
in the transcript, supported meaningful transcript roles, strictly increasing
message indices and timestamps, and each B prescription after the final A message.
It also requires positive and zero cases plus complete A/B pairs before any model
call. The committed corpus stores ten score cases and five compact pair definitions.
The error-context and forbidden-PR-section labels each accept the two-outcome
set `partially_helped` or `did_not_help`; legacy-location accepts the two-outcome
set `misleading` or `harmful`. Every other label has exactly one reviewed
outcome, including `partially_helped` for chat-release-native-formatting.

Each pair compares the same direct evidence before (`A`) and after (`B`) a user
explicitly prescribes the correction. `A` expects one structured recommendation.
`B` expects none, testing complete suppression of that same problem, including
evidence-only feedback when the skill still performed poorly. A distinct
unprescribed problem may still be recommended.

`-baseline` accepts a sanitized file containing exactly one model and one nonempty
prompt version. Every row must match the current recommendation schema version
and requested reasoning effort. Per-case drift reports both prompt versions.
Prompt or rubric changes must increment `efficacy.JudgePromptVersion`; label-shape
changes must increment `recommendation_schema_version`.

## Results

The `v11` corpus replaces v10 note aliases with reviewed structured taxonomy and
grounding labels while preserving score anchors, metadata, suppression pairs,
privacy, strict quorum, and gates. A three-run comparison produced:

| Model                            | Score | Positive | Suppression | Pairs | Recommendation cost | Recommendation p95 |
| -------------------------------- | ----: | -------: | ----------: | ----: | ------------------: | -----------------: |
| `google/gemini-3.1-flash-lite`   | 80.0% |    62.5% |       66.7% | 20.0% |           $0.000736 |             1.337s |
| `openai/gpt-5.6-terra`           | 70.0% |    87.5% |       83.3% | 80.0% |           $0.001818 |             2.747s |
| `z-ai/glm-5.3-flash` (`minimal`) | 80.0% |    62.5% |       91.7% | 40.0% |           $0.000127 |             6.674s |

No configuration passed every gate. Terra had the strongest recommendation and
pair agreement but missed the score and persistence-precision gates. Gemini met
the score gate with the lowest latency but repeated prescribed remedies too
often. GLM was cheapest and had the strongest suppression, but missed the
positive, pair, and persistence-precision gates.
