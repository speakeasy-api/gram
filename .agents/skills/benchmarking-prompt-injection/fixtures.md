# Changing prompt-injection fixtures

Read with `SKILL.md` in this folder. Fixtures live in `server/internal/scanners/promptinjection/testdata/prompt_injection/`; a fixture change needs a gate run, but `mise run risk:pi` reruns only new or edited cases.

- Copy an existing line from the same file. Every case has an `id` unique within its file, a `label` (`benign` or `malicious`), `text` and `source`. `litellm_extended` and `adversarial_*` rows also need `directive_present`; `mutations` rows carry `seed_id` instead and inherit it. Edit `mutations.jsonl` by hand: `gen:risk-mutations` writes to an old path. Fixtures are public, so never add customer data.
- Tag an attack that uses a classic phrase with `well_known`, naming the phrase: "Ignore previous instructions", "Reveal the system prompt", "DAN or developer mode", "Jailbreak or unrestricted AI", "Fake system override" or "German version".
- A row that repeats an earlier row's text is dropped silently (except in `trajectory_twins` and `agentdojo`). Every `.jsonl` file in the folder is scored, and one `corpusOrder` in `server/cmd/risk-pi-report/main.go` does not list loads last, in name order. Keep cases the gate must not score, such as the cascade smoke cases in `testdata/cascade_smoke/`, in another folder.
- Update the counts:
  - `TestGateCorpusMatchesEvaluationReport` in `server/cmd/risk-pi-report/gate_test.go` (2,046 cases, 975 attacks, 169 well-known);
  - the tables and prose counts in `corpus_notes.md`;
  - the gate paragraph of CASCADE.md and the numbers in `SKILL.md`. The gate needs 80% of the attacks, rounded up: the current 975 attacks need 780; for example, adding one attack would make 976 attacks need 781.
