# AIS-415 and AIS-413 PI Judge Eval Results

Generated: 2026-08-04T12:47:02Z

Model: `google/gemini-3.5-flash-lite`

Provider pinned with OpenRouter `provider.order=["Google AI Studio"]`, `allow_fallbacks=false`, `require_parameters=true`. Temperature was 0. Each variant used 3 repeated runs. Raw artifacts are in `risk-pi-report/ais-415-413`.

Corpus rows: 1190 committed rows and 12 synthetic assistant-intent rows. Expected positives use `directive_present` where present, otherwise `label == malicious`.

## Methodology

- Baseline used the production typed payload and production system prompt.
- No trajectory removed `prior_user_request` and `recent_untrusted_content`.
- Flat payload rendered the same message fields as a single plain text string.
- Simple prompt used a shorter directive-detection prompt.
- Assistant intent added `assistant_intent` and a prompt sentence that treats it as untrusted evidence.
- Malformed completions were counted as fail-open safe votes, matching production behavior. Seven samples hit that path.

## Per-Corpus Metrics

| Variant | Group | Corpus | Run | N | Pos | Neg | Recall | FPR | Determinism | Errors |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| baseline | adversarial | adversarial_codex | 1 | 50 | 42 | 8 | 1.000 | 0.625 | 0.940 | 0 |
| baseline | adversarial | adversarial_codex | 2 | 50 | 42 | 8 | 0.976 | 0.625 | 0.940 | 0 |
| baseline | adversarial | adversarial_codex | 3 | 50 | 42 | 8 | 1.000 | 0.750 | 0.940 | 0 |
| baseline | adversarial | adversarial_fable | 1 | 50 | 41 | 9 | 0.976 | 0.444 | 0.940 | 0 |
| baseline | adversarial | adversarial_fable | 2 | 50 | 41 | 9 | 0.976 | 0.444 | 0.940 | 0 |
| baseline | adversarial | adversarial_fable | 3 | 50 | 41 | 9 | 1.000 | 0.444 | 0.940 | 0 |
| baseline | organic | agent_fp_benigns | 1 | 83 | 0 | 83 | 0.000 | 0.000 | 1.000 | 0 |
| baseline | organic | agent_fp_benigns | 2 | 83 | 0 | 83 | 0.000 | 0.000 | 1.000 | 0 |
| baseline | organic | agent_fp_benigns | 3 | 83 | 0 | 83 | 0.000 | 0.000 | 1.000 | 0 |
| baseline | organic | deepset | 1 | 662 | 263 | 399 | 0.445 | 0.000 | 0.971 | 0 |
| baseline | organic | deepset | 2 | 662 | 263 | 399 | 0.452 | 0.000 | 0.971 | 0 |
| baseline | organic | deepset | 3 | 662 | 263 | 399 | 0.471 | 0.000 | 0.971 | 0 |
| baseline | organic | gram_benigns | 1 | 140 | 0 | 140 | 0.000 | 0.000 | 1.000 | 0 |
| baseline | organic | gram_benigns | 2 | 140 | 0 | 140 | 0.000 | 0.000 | 1.000 | 0 |
| baseline | organic | gram_benigns | 3 | 140 | 0 | 140 | 0.000 | 0.000 | 1.000 | 0 |
| baseline | organic | litellm_extended | 1 | 51 | 48 | 3 | 0.938 | 0.000 | 0.941 | 0 |
| baseline | organic | litellm_extended | 2 | 51 | 48 | 3 | 0.917 | 0.000 | 0.941 | 1 |
| baseline | organic | litellm_extended | 3 | 51 | 48 | 3 | 0.938 | 0.000 | 0.941 | 0 |
| baseline | organic | mutations | 1 | 70 | 70 | 0 | 0.986 | 0.000 | 0.971 | 0 |
| baseline | organic | mutations | 2 | 70 | 70 | 0 | 0.986 | 0.000 | 0.971 | 0 |
| baseline | organic | mutations | 3 | 70 | 70 | 0 | 0.986 | 0.000 | 0.971 | 0 |
| baseline | organic | operational_benigns | 1 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| baseline | organic | operational_benigns | 2 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| baseline | organic | operational_benigns | 3 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| baseline | trajectory_twins | trajectory_twins | 1 | 74 | 37 | 37 | 0.973 | 0.000 | 0.973 | 0 |
| baseline | trajectory_twins | trajectory_twins | 2 | 74 | 37 | 37 | 0.946 | 0.000 | 0.973 | 1 |
| baseline | trajectory_twins | trajectory_twins | 3 | 74 | 37 | 37 | 0.946 | 0.027 | 0.973 | 0 |
| flat_payload | adversarial | adversarial_codex | 1 | 50 | 42 | 8 | 0.952 | 0.625 | 0.940 | 0 |
| flat_payload | adversarial | adversarial_codex | 2 | 50 | 42 | 8 | 0.976 | 0.750 | 0.940 | 0 |
| flat_payload | adversarial | adversarial_codex | 3 | 50 | 42 | 8 | 0.976 | 0.750 | 0.940 | 0 |
| flat_payload | adversarial | adversarial_fable | 1 | 50 | 41 | 9 | 1.000 | 0.667 | 0.960 | 0 |
| flat_payload | adversarial | adversarial_fable | 2 | 50 | 41 | 9 | 1.000 | 0.667 | 0.960 | 0 |
| flat_payload | adversarial | adversarial_fable | 3 | 50 | 41 | 9 | 1.000 | 0.444 | 0.960 | 1 |
| flat_payload | organic | agent_fp_benigns | 1 | 83 | 0 | 83 | 0.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | agent_fp_benigns | 2 | 83 | 0 | 83 | 0.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | agent_fp_benigns | 3 | 83 | 0 | 83 | 0.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | deepset | 1 | 662 | 263 | 399 | 0.513 | 0.000 | 0.967 | 0 |
| flat_payload | organic | deepset | 2 | 662 | 263 | 399 | 0.494 | 0.000 | 0.967 | 0 |
| flat_payload | organic | deepset | 3 | 662 | 263 | 399 | 0.510 | 0.000 | 0.967 | 0 |
| flat_payload | organic | gram_benigns | 1 | 140 | 0 | 140 | 0.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | gram_benigns | 2 | 140 | 0 | 140 | 0.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | gram_benigns | 3 | 140 | 0 | 140 | 0.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | litellm_extended | 1 | 51 | 48 | 3 | 0.979 | 0.333 | 0.961 | 0 |
| flat_payload | organic | litellm_extended | 2 | 51 | 48 | 3 | 1.000 | 0.667 | 0.961 | 0 |
| flat_payload | organic | litellm_extended | 3 | 51 | 48 | 3 | 1.000 | 0.667 | 0.961 | 0 |
| flat_payload | organic | mutations | 1 | 70 | 70 | 0 | 1.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | mutations | 2 | 70 | 70 | 0 | 1.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | mutations | 3 | 70 | 70 | 0 | 1.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | operational_benigns | 1 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | operational_benigns | 2 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| flat_payload | organic | operational_benigns | 3 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| flat_payload | trajectory_twins | trajectory_twins | 1 | 74 | 37 | 37 | 0.973 | 0.000 | 0.973 | 0 |
| flat_payload | trajectory_twins | trajectory_twins | 2 | 74 | 37 | 37 | 1.000 | 0.000 | 0.973 | 0 |
| flat_payload | trajectory_twins | trajectory_twins | 3 | 74 | 37 | 37 | 0.973 | 0.000 | 0.973 | 1 |
| no_trajectory | adversarial | adversarial_codex | 1 | 50 | 42 | 8 | 0.976 | 0.625 | 0.940 | 0 |
| no_trajectory | adversarial | adversarial_codex | 2 | 50 | 42 | 8 | 0.976 | 0.625 | 0.940 | 0 |
| no_trajectory | adversarial | adversarial_codex | 3 | 50 | 42 | 8 | 1.000 | 0.750 | 0.940 | 0 |
| no_trajectory | adversarial | adversarial_fable | 1 | 50 | 41 | 9 | 1.000 | 0.556 | 0.940 | 0 |
| no_trajectory | adversarial | adversarial_fable | 2 | 50 | 41 | 9 | 0.976 | 0.333 | 0.940 | 1 |
| no_trajectory | adversarial | adversarial_fable | 3 | 50 | 41 | 9 | 1.000 | 0.444 | 0.940 | 0 |
| no_trajectory | organic | agent_fp_benigns | 1 | 83 | 0 | 83 | 0.000 | 0.000 | 0.988 | 0 |
| no_trajectory | organic | agent_fp_benigns | 2 | 83 | 0 | 83 | 0.000 | 0.000 | 0.988 | 0 |
| no_trajectory | organic | agent_fp_benigns | 3 | 83 | 0 | 83 | 0.000 | 0.012 | 0.988 | 0 |
| no_trajectory | organic | deepset | 1 | 662 | 263 | 399 | 0.449 | 0.000 | 0.971 | 0 |
| no_trajectory | organic | deepset | 2 | 662 | 263 | 399 | 0.460 | 0.000 | 0.971 | 0 |
| no_trajectory | organic | deepset | 3 | 662 | 263 | 399 | 0.456 | 0.000 | 0.971 | 0 |
| no_trajectory | organic | gram_benigns | 1 | 140 | 0 | 140 | 0.000 | 0.000 | 1.000 | 0 |
| no_trajectory | organic | gram_benigns | 2 | 140 | 0 | 140 | 0.000 | 0.000 | 1.000 | 0 |
| no_trajectory | organic | gram_benigns | 3 | 140 | 0 | 140 | 0.000 | 0.000 | 1.000 | 0 |
| no_trajectory | organic | litellm_extended | 1 | 51 | 48 | 3 | 0.917 | 0.333 | 0.882 | 0 |
| no_trajectory | organic | litellm_extended | 2 | 51 | 48 | 3 | 0.917 | 0.333 | 0.882 | 1 |
| no_trajectory | organic | litellm_extended | 3 | 51 | 48 | 3 | 0.917 | 0.000 | 0.882 | 0 |
| no_trajectory | organic | mutations | 1 | 70 | 70 | 0 | 0.986 | 0.000 | 0.986 | 0 |
| no_trajectory | organic | mutations | 2 | 70 | 70 | 0 | 0.971 | 0.000 | 0.986 | 0 |
| no_trajectory | organic | mutations | 3 | 70 | 70 | 0 | 0.971 | 0.000 | 0.986 | 0 |
| no_trajectory | organic | operational_benigns | 1 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| no_trajectory | organic | operational_benigns | 2 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| no_trajectory | organic | operational_benigns | 3 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| no_trajectory | trajectory_twins | trajectory_twins | 1 | 74 | 37 | 37 | 0.730 | 0.000 | 0.986 | 0 |
| no_trajectory | trajectory_twins | trajectory_twins | 2 | 74 | 37 | 37 | 0.703 | 0.000 | 0.986 | 0 |
| no_trajectory | trajectory_twins | trajectory_twins | 3 | 74 | 37 | 37 | 0.703 | 0.000 | 0.986 | 1 |
| simple_prompt | adversarial | adversarial_codex | 1 | 50 | 42 | 8 | 1.000 | 0.375 | 0.980 | 0 |
| simple_prompt | adversarial | adversarial_codex | 2 | 50 | 42 | 8 | 1.000 | 0.250 | 0.980 | 0 |
| simple_prompt | adversarial | adversarial_codex | 3 | 50 | 42 | 8 | 1.000 | 0.375 | 0.980 | 0 |
| simple_prompt | adversarial | adversarial_fable | 1 | 50 | 41 | 9 | 1.000 | 0.444 | 0.920 | 0 |
| simple_prompt | adversarial | adversarial_fable | 2 | 50 | 41 | 9 | 1.000 | 0.222 | 0.920 | 0 |
| simple_prompt | adversarial | adversarial_fable | 3 | 50 | 41 | 9 | 1.000 | 0.333 | 0.920 | 0 |
| simple_prompt | organic | agent_fp_benigns | 1 | 83 | 0 | 83 | 0.000 | 0.084 | 0.988 | 0 |
| simple_prompt | organic | agent_fp_benigns | 2 | 83 | 0 | 83 | 0.000 | 0.084 | 0.988 | 0 |
| simple_prompt | organic | agent_fp_benigns | 3 | 83 | 0 | 83 | 0.000 | 0.096 | 0.988 | 0 |
| simple_prompt | organic | deepset | 1 | 662 | 263 | 399 | 0.586 | 0.000 | 0.974 | 0 |
| simple_prompt | organic | deepset | 2 | 662 | 263 | 399 | 0.567 | 0.000 | 0.974 | 0 |
| simple_prompt | organic | deepset | 3 | 662 | 263 | 399 | 0.563 | 0.000 | 0.974 | 0 |
| simple_prompt | organic | gram_benigns | 1 | 140 | 0 | 140 | 0.000 | 0.007 | 0.993 | 0 |
| simple_prompt | organic | gram_benigns | 2 | 140 | 0 | 140 | 0.000 | 0.007 | 0.993 | 0 |
| simple_prompt | organic | gram_benigns | 3 | 140 | 0 | 140 | 0.000 | 0.000 | 0.993 | 0 |
| simple_prompt | organic | litellm_extended | 1 | 51 | 48 | 3 | 0.979 | 1.000 | 0.980 | 0 |
| simple_prompt | organic | litellm_extended | 2 | 51 | 48 | 3 | 1.000 | 1.000 | 0.980 | 0 |
| simple_prompt | organic | litellm_extended | 3 | 51 | 48 | 3 | 1.000 | 1.000 | 0.980 | 0 |
| simple_prompt | organic | mutations | 1 | 70 | 70 | 0 | 1.000 | 0.000 | 1.000 | 0 |
| simple_prompt | organic | mutations | 2 | 70 | 70 | 0 | 1.000 | 0.000 | 1.000 | 0 |
| simple_prompt | organic | mutations | 3 | 70 | 70 | 0 | 1.000 | 0.000 | 1.000 | 0 |
| simple_prompt | organic | operational_benigns | 1 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| simple_prompt | organic | operational_benigns | 2 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| simple_prompt | organic | operational_benigns | 3 | 10 | 0 | 10 | 0.000 | 0.000 | 1.000 | 0 |
| simple_prompt | trajectory_twins | trajectory_twins | 1 | 74 | 37 | 37 | 0.973 | 0.027 | 0.986 | 0 |
| simple_prompt | trajectory_twins | trajectory_twins | 2 | 74 | 37 | 37 | 0.973 | 0.027 | 0.986 | 0 |
| simple_prompt | trajectory_twins | trajectory_twins | 3 | 74 | 37 | 37 | 0.973 | 0.054 | 0.986 | 0 |

## Assistant Intent Metrics

| Variant | Group | Corpus | Run | N | Pos | Neg | Recall | FPR | Determinism | Errors |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| assistant_intent | intent_spoof | assistant_intent_twins | 1 | 3 | 2 | 1 | 1.000 | 0.000 | 0.667 | 0 |
| assistant_intent | organic | assistant_intent_twins | 1 | 9 | 3 | 6 | 1.000 | 0.000 | 1.000 | 0 |
| assistant_intent | organic | assistant_intent_twins | 2 | 9 | 3 | 6 | 1.000 | 0.000 | 1.000 | 0 |
| assistant_intent | intent_spoof | assistant_intent_twins | 2 | 3 | 2 | 1 | 1.000 | 1.000 | 0.667 | 0 |
| assistant_intent | organic | assistant_intent_twins | 3 | 9 | 3 | 6 | 1.000 | 0.000 | 1.000 | 0 |
| assistant_intent | intent_spoof | assistant_intent_twins | 3 | 3 | 2 | 1 | 1.000 | 0.000 | 0.667 | 0 |

## Component Verdicts

- Trajectory context: keep. Delta recall -0.008, delta FPR 0.045 against baseline on non-adversarial corpora.
- Typed payload: keep. Flat payload delta recall 0.042, delta FPR 0.111 against baseline on non-adversarial corpora.
- Full prompt: keep. Simple prompt delta recall 0.064, delta FPR 0.219 against baseline on non-adversarial corpora.

## AIS-413 Go / No-Go

Recommendation: **no-go** for `assistant_intent` as untrusted typed context. Intent twin recall 1.000, FPR 0.000. Spoofing split recall 1.000, FPR 0.333.

Best tested payload shape:

```json
{"message":{...},"trajectory":{...},"assistant_intent":"assistant text accompanying the tool call"}
```
