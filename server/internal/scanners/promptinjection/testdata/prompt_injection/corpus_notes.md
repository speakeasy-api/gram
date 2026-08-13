# Prompt-injection accuracy corpus notes

This directory holds the taxonomy v2 corpus consumed by `mise risk:report`.
Rows are sparse JSONL records. Per-file provenance and metric population live in `manifest.json`.

## Row taxonomy

Required row fields:

- `id`, `text`
- `surface`: `user_message`, `assistant_message`, `tool_request`, or `tool_response`
- `directive_present`: the ground-truth bit scored by the judge

Attack facets appear only when `directive_present=true`: `carrier`, `technique`, and `goal`.
Benign FP-gate rows carry `fp_category` instead. Context fields retain their runtime meaning: `tool`, `prior_user_request`, and `recent_untrusted_content`. Lineage fields are `twin_of`, `seed_id`, and `known_gap`.

The loader rejects old `label`, `source`, and `type` fields, unknown facet values, missing manifest entries, and facet contradictions.

## File layout

File boundaries are based on gate and regeneration workflow.

| File | Gate | Rows | Workflow |
| --- | --- | ---: | --- |
| `curated_benigns.jsonl` | fp | 233 | Hand-curated benign hard negatives merged from `gram_benigns`, `operational_benigns`, and `agent_fp_benigns`. |
| `curated_adversarial.jsonl` | recall | 100 | Curated model-authored adversarial rows merged from fable and codex predecessors; per-row `origin` keeps authorship auditable. |
| `litellm_extended.jsonl` | recall | 51 | Hand-curated rows inspired by LiteLLM injection fixtures. |
| `mutations.jsonl` | recall | 70 | Deterministically generated mutation rows with `seed_id` lineage. |
| `trajectory_twins.jsonl` | recall | 74 | Paired trajectory rows with runtime context and known-gap exclusions. |
| `deepset.jsonl` | regression | 662 | External import reported separately due taxonomy mismatch. |
| `notinject.jsonl` | fp | 339 | External benign trigger-vocabulary import from NotInject. |
| `llmail_hard.jsonl` | recall | 200 | External LLMail hard attacks, pending curation and excluded from enforced floors. |
| `agentdojo_recall.jsonl` | recall | 27 | Static AgentDojo attack extraction, pending curation and excluded from enforced floors. |
| `agentdojo_fp.jsonl` | fp | 27 | Static AgentDojo benign twins, pending curation and excluded from enforced floors. |
| `url_cue_benigns.jsonl` | fp | 8 | Curated reserved-domain benign rows added for surface-cue balance. |

`discarded_rows.md` records merge dedupe/discard decisions. Current reorganization discarded no rows.

## Gates

Headline recall uses reviewed `gate=recall` rows where `directive_present=true`, excluding `known_gap` rows and pending-review imports.
Headline false-positive rate uses reviewed `gate=fp` rows where `directive_present=false`, excluding pending-review imports.
`gate=regression` rows are reported separately and never enter headline recall or FPR.

Facet reports break recall down by `carrier`, `technique`, and `goal`, and FPR by `fp_category`.

## External sources

License checks at ingest time:

- NotInject from SaFoLab-WISC/InjecGuard: MIT.
- LLMail inject challenge from `microsoft/llmail-inject-challenge`: MIT.
- AgentDojo from `ethz-spylab/agentdojo`: MIT.

The migration script scrubs attack URLs and provider names into reserved-domain or generic placeholders. Synthetic secrets remain fake.

## Regeneration

Run:

```sh
./server/internal/scanners/promptinjection/testdata/prompt_injection/scripts/migrate_taxonomy_v2.py
```

The script migrates existing rows, rebuilds merged files from predecessor files in git history when needed, fetches external imports, validates taxonomy constraints, writes `manifest.json`, and refreshes `discarded_rows.md`.

`mutations.jsonl` remains tied to deterministic seed lineage. If LiteLLM seed annotations change, rerun the migration so mutation `directive_present` values stay synchronized.

## Floors

`floors.json` enforces recall over reviewed recall-gate sources. The merged `curated_adversarial` source starts at the maximum of predecessor floors until a new 5-run baseline is taken.

The FP gate is re-enabled over reviewed FP-gate rows with a lenient initial ceiling. Tighten it after a 5-run baseline.
