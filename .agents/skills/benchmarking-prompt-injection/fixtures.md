# Changing prompt-injection fixtures

Read with `SKILL.md` in this folder. Fixtures live in `server/internal/scanners/promptinjection/testdata/prompt_injection/`; every fixture change needs a gate run.

- Copy an existing line from the same file. Every case has an `id` unique within its file, a `label` (`benign` or `malicious`), `text` and `source`. `litellm_extended` and `adversarial_*` rows also need `directive_present`; `mutations` rows carry `seed_id` instead and inherit it from that LiteLLM row. Edit `mutations.jsonl` by hand: `gen:risk-mutations` writes to an old path. Fixtures are public, so never add customer data.
- A row that repeats an earlier row's text is dropped silently (except in `trajectory_twins` and `agentdojo`). A new `.jsonl` file loads only when `requiredCorpusFiles` or `optionalCorpusFiles` in `server/cmd/risk-pi-report/main.go` lists it.
- New fixtures and relabels change the counts; a relabel moves a case between them. Update:
  - `TestGateCorpusMatchesEvaluationReport` in `server/cmd/risk-pi-report/gate_test.go` (2,046 cases, 975 attacks), and `TestCommittedRecallFixturesUseReviewedDirectiveTaxonomy` in `main_test.go` for `litellm_extended`, `adversarial_*` and `mutations`;
  - the counts table and the prose counts in `corpus_notes.md`;
  - the gate paragraph of CASCADE.md (not its report figures), the comment in `.mise-tasks/risk/pi-gate.sh`, the numbers and example output in `SKILL.md`, and the counts in this file. The required count is 80% of the attacks, rounded up: 976 attacks need 781.
- The free tests (`mise run test:server ./cmd/risk-pi-report/`) catch a missed count in the Go pins only; check the docs by hand.
