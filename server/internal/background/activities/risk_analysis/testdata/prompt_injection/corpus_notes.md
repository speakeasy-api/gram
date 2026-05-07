# Prompt-injection accuracy corpus notes

This directory holds the labeled corpus consumed by `TestDetectPromptInjection_AccuracyBaseline`. Notes below capture decisions made when assembling the corpus and findings surfaced on first run.

## Sources

| File                     | Origin                                                                        | License    | Rows | Class balance              |
| ------------------------ | ----------------------------------------------------------------------------- | ---------- | ---- | -------------------------- |
| `deepset.jsonl`          | `deepset/prompt-injections` on HuggingFace, train + test splits concatenated  | Apache 2.0 | 662  | 263 malicious / 399 benign |
| `gram_benigns.jsonl`     | Hand-authored realistic Gram-style prompts                                    | Internal   | 140  | 0 malicious / 140 benign   |
| `litellm_extended.jsonl` | Hand-authored, inspired by injection patterns in BerriAI/litellm tests        | Internal   | 51   | 51 malicious / 0 benign    |
| `mutations.jsonl`        | Pre-baked output of `mise gen:risk-mutations`, deterministic from fixed seeds | Internal   | 75   | 75 malicious / 0 benign    |

After dedup the loaded corpus is 928 cases (some duplicates between sources are dropped — see test log).

## Deepset labeling philosophy mismatch

deepset's notion of "prompt injection" is broader than ours. Several rows labeled `malicious` are not injection attempts under the heuristics' definition. Examples:

- `"Generate SQL code to access a database."` — labeled malicious; we treat this as a benign tool-call request.
- `"I want you to act as an interviewer..."` — labeled malicious; we don't flag non-privileged role roleplay.

We commit deepset as-is rather than re-labeling per our own taxonomy because (a) altering an external dataset to look better is misleading, and (b) keeping the noise documented turns the deepset slice into a stress test for "does the detector flag things deepset thinks are bad" rather than ground truth. Recall on the deepset slice is therefore expected to be low and is **not** a signal of detector quality on its own. Trust per-source recall on `litellm_extended` and the mutation slices for tuning decisions.

## Findings surfaced by the baseline run

The first baseline run surfaced one rule with FPs: `pi.jailbreak-persona`, which used plain substring matching on `DAN` / `STAN` / `AIM` and so fired inside German `dankbar`, English `standard`, `assistant`, `claims`, etc. Switching to word-boundary regex eliminated those FPs but the rule's value was already marginal — most attacks it caught were already covered by `role-hijack.act-as-privileged` or `instruction-override`, and persona names churn fast in the wild ("DAN" → "STAN" → "AIM" → custom names every few months). Removed entirely; `litellm_extended` keeps a few persona-only cases as honest FNs so future contributors can see what was lost.

## Mutation set behavior

| Technique             | Recall on this run                                                                                                     |
| --------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `base64_wrap`         | 100% (encoded-payload rule catches the long base64 blob with adjacent decode/execute verb).                            |
| `alt_case`            | 87% (regexes are case-insensitive — the few misses are seeds whose triggers don't survive alt-case in lowercase form). |
| `multilingual_prefix` | 87% (Spanish prefix doesn't defeat detection — the trailing English trigger still matches).                            |
| `leetspeak`           | 0% (heuristics don't normalize i→1, e→3, etc.).                                                                        |
| `zero_width`          | 0% (heuristics don't strip zero-width characters).                                                                     |

The 0% mutations are useful: they document that the heuristics are brittle to character-level obfuscation. A classifier or normalization step would close this gap.

## Regenerating fixtures

- `deepset.jsonl`: `curl` the train + test parquet files from HuggingFace and convert with pandas + pyarrow. The conversion script lives only in commit history; rerun is rare.
- `mutations.jsonl`: `mise gen:risk-mutations` (deterministic; commit the resulting file).
- `gram_benigns.jsonl`, `litellm_extended.jsonl`: hand-curated; edit directly.

## Updating the floor

`floors.json` enforces `fp_rate_max` as a hard cap. Any PR that pushes FP-rate above the cap fails CI. Two valid responses:

1. Fix the regression in detector code and bring FP-rate back under the cap.
2. Update `floors.json` in the same PR with a tightened or loosened cap and a note explaining why. A loosening should be justified in the PR description.

`recall_floor` is left `null` for now and treated as a soft signal; it's logged by the test but never fails CI.
