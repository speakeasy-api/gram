# Magellan

Magellan explores the dashboard every night, the way a user would, and files
the bugs it can prove as Linear Triage issues. It complements the flow checks
in `../flows`: those protect flows we already know; Magellan finds the ones
that break where nobody wrote a check.

## How a run works

1. `mise run magellan:explore` lists dashboard files changed on `main` since
   yesterday (`focus.md`), then hands `EXPLORE.md` to a Claude Code agent.
2. The agent explores those pages first, then roams. It watches the console
   and every `/rpc/` call while it clicks.
3. It records a finding only with hard evidence — a console error, a failed
   RPC, a dead end, a race, or a control with no accessible name — and only
   after it reproduces from a fresh page load. Taste goes to `notes.md`, not
   to Linear.
4. `mise run magellan:file` turns findings into Linear Triage issues. Each
   carries a fingerprint built from kind, route, and the agent's stable
   `signature`, so the same bug found on two nights stays one issue. Without
   `--file` it only prints what it would file.
5. Each issue includes a draft `story.md`. Once the bug is fixed, commit that
   story under `e2e/flows/` and the flow checks keep it fixed.

Runs land in `e2e/.magellan/<timestamp>/` (git-ignored): `focus.md`,
`findings.jsonl`, `notes.md`, screenshots, and the agent transcript.

## Running it locally

```bash
mise run magellan:explore --since "3 days ago" --budget 20
mise run magellan:file                 # dry run: prints the issues
mise run magellan:file --file          # needs LINEAR_API_KEY
```

CI runs both on weeknights (`.github/workflows/magellan.yml`).

## Not yet

The first local run reported findings that turned out to be artifacts of the
local stack (feature gates off, state left by earlier test runs). Magellan
should move to a real environment, such as a dedicated integration org in
production, where what it sees is what users see.

Magellan files issues only. Opening draft fix PRs comes once its findings
have proven trustworthy for a few weeks.
