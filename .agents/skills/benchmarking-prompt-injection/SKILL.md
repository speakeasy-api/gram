---
name: benchmarking-prompt-injection
description: Use when changing or reviewing prompt-injection detection in server/internal/scanners/promptinjection, server/cmd/risk-pi-report, the prompt_injection fixtures, server/internal/judgemessage, GetJudgeMessageWindow in server/internal/risk/queries.sql, trajectory building in server/internal/background/activities/risk_analysis, or server/internal/hooks skill-upload handling. Triggers: "prompt injection", "Jev", "confirmer", "risk:pi", "false positive", "recall", "relabel".
---

# Benchmarking prompt-injection changes

A change that can alter a verdict merges only after `mise run risk:pi` passes on the final commit: **0 false positives** and **at least 80% of attacks caught** (780 of 975). It runs this change and main side by side, about $2.30 per side, so run it locally and paste its summary in the PR.

- **Needs it:** prompts, `PrefilterThreshold`, models, evidence rendering, truncation, timeouts, retries, fixtures.
- **Doesn't:** comments, docs, logging, tests, pure renames. Write `PI gate not run: <reason>`.
- **Can't see:** production window or trajectory assembly, and skill-upload hook handling. Test that code directly.

## Commands

- `mise run zero:openrouter` once; keep about $5 of OpenRouter credit.
- `mise run risk:pi`: full run, merge gate, `report.html`, and a Markdown summary for the PR. A commit that changes only fixtures reuses every unedited case's result, so iterating on a draft costs only the cases it changes.
- `--watch` serves a live viewer; `--view` opens cached results; `--summary-md` prints the PR table; `--no-main` skips main.
- The viewer lists every cached run, newest first, and compares any two; it opens on main and this change.
- Rerun the same command to resume, including after running out of credit. A base from before per-case records fails before scoring; choose a newer `--base <ref>` or explicitly use `--no-main` for a one-sided run.

Agents: never read `mise.local.toml`; ask before committing and before paid runs.

Failures: `failures.md`. Fixtures: `fixtures.md`. Reviews: `reviewing.md`.
