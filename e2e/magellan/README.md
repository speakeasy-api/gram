# Magellan

Magellan explores the dashboard every night, the way a user would, and files
the bugs it can prove as Linear Triage issues. It complements the flow checks
in `../flows`: those protect flows we already know; Magellan finds the ones
that break where nobody wrote a check.

## How a run works

1. A Claude Code agent gets the list of dashboard files changed on `main`
   since yesterday (paths only) and the prompt in
   `.github/prompts/magellan-explore.md`.
2. It explores those pages first, then roams, watching the console and every
   `/rpc/` call while it clicks.
3. It records a finding only with hard evidence — a console error, a failed
   RPC, a dead end, a race, or a control with no accessible name — and only
   after it reproduces from a fresh page load.
4. `mise run magellan:file` turns findings into Linear Triage issues. Each
   carries a fingerprint built from kind, route, and the agent's stable
   `signature`, so the same bug found on two nights stays one issue.
5. Each issue includes a draft `story.md`. Once the bug is fixed, commit that
   story under `e2e/flows/` and the flow checks keep it fixed.

## Where it runs

**Nightly, against production, in a dedicated integration org**, as a
[GitHub Agentic Workflow](https://github.github.com/gh-aw/) in
`speakeasy-api/gram-infra` (`.github/workflows/magellan.md`). It lives there,
not here, because gh-aw uploads every run's agent transcript as an artifact,
and this repository is public: the transcript holds production pages.

That workflow checks out this repository's `main` for the prompt and the
dashboard source, and runs the agent in gh-aw's AWF sandbox: its network is
the dashboard host only, and the Anthropic key stays with AWF's API proxy.
Findings leave the agent only through a `file_finding` safe output, which a
separate job (holding the Linear key) passes to
`mise run magellan:file --agent-output`. That script validates each finding,
puts agent text into Linear only inside code blocks, and logs nothing the
agent wrote.

**Locally**, for trying out prompt changes against your own stack. This is not
sandboxed: the agent runs as you.

```bash
mise run magellan:explore --since "3 days ago" --budget 20
mise run magellan:file                 # dry run: prints the issues
mise run magellan:file --file          # needs LINEAR_API_KEY
```

## The bot user

Production login goes through WorkOS and cannot be scripted, so the nightly
run reuses a real `gram_session` for a bot user. It stays valid while it is
used at least once every 72 hours; a daily `keepalive` workflow in
`gram-infra` uses it. Setup (bot user, capturing the session, the `magellan`
environment and its secrets) is in that workflow's header.

The agent's rules for a shared org: it names what it creates
`Magellan <run id> ...`, edits or deletes only data with its own run's id,
deletes that data before it finishes, never changes org settings, and never
sends anything outside the product.
