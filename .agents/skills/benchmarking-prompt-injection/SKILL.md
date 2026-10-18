---
name: benchmarking-prompt-injection
description: Use when changing or reviewing prompt-injection detection in server/internal/scanners/promptinjection, server/cmd/risk-pi-report, the prompt_injection fixtures, server/internal/judgemessage, GetJudgeMessageWindow in server/internal/risk/queries.sql, trajectory building in server/internal/background/activities/risk_analysis, or server/internal/hooks skill-upload handling. Triggers: "prompt injection", "Jev", "confirmer", "risk:pi", "false positive", "recall", "relabel".
---

# Benchmarking prompt-injection changes

Run the paid `mise run risk:pi` benchmark only when changing the prompt-injection detector's prompts or models. Those changes merge only after it passes on the final commit: **0 false positives** and **at least 80% of attacks caught** (780 of 975). A run costs about $2.30; run it locally with `--compare main` and paste its summary in the PR.

- **Needs it:** changes to the detector's prompts or models.
- **Doesn't:** other changes, including upload/ingestion wiring, attachment resolution, policy enforcement, thresholds, evidence assembly, truncation, timeouts, retries, fixtures, or refactors that leave detector prompts and models unchanged. Test the affected behavior directly; do not request a paid benchmark just because production inputs or enforcement outcomes can change.
- Loading this skill or touching a listed directory does not itself require the benchmark. For a relevant PR that does not change detector prompts or models, write `PI gate not run: detector prompts and models unchanged; <targeted verification>`.

## Commands

- `mise run zero:openrouter` once; keep about $5 of OpenRouter credit.
- `mise run risk:pi`: a new run of this change, the merge gate, `report.html`, and a Markdown summary. Every run is new.
- `--compare main`: also scores the latest main, reusing its latest run, and compares the two case by case. Use it for the PR summary. A ref from before per-case records fails before scoring.
- `--resume`: continues the latest run of this commit, such as after running out of credit, even after uncommitted edits. After a fixture edit it runs only the cases the edit changed.
- `--watch` serves a live viewer; `--view` opens cached results; `--summary-md` prints the PR table. Add `--compare main` to any of them for the comparison.
- The viewer lists every cached run, newest first, and compares any two; it opens on this change and the `--compare` ref.

Agents: never read `mise.local.toml`; ask before committing and before paid runs.

Failures: `failures.md`. Fixtures: `fixtures.md`. Reviews: `reviewing.md`.
