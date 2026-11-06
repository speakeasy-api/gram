# Magellan

Magellan explores the dashboard every night, the way a user would, and files
the bugs it can prove as Linear Triage issues. It complements the flow checks
in `../flows`: those protect flows we already know; Magellan finds the ones
that break where nobody wrote a check.

## How a run works

1. `mise run magellan:explore` lists dashboard files changed on `main` since
   yesterday (`focus.md`, paths only), then hands `EXPLORE.md` to a Claude Code agent.
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

Runs land in `e2e/.magellan/<timestamp>/` (git-ignored). That directory is
the agent's workspace: `context/` holds copies of what it may read (the
focus list, the dashboard source, the flow stories) and `out/` is the only
place it may write (`findings.jsonl`, `notes.md`, screenshots).

## Where it runs

**Production, in a dedicated integration org.** The first local run reported
findings that were artifacts of the local stack (feature gates off, state
left by earlier test runs). In production, what Magellan sees is what users
see.

`.github/workflows/magellan.yml` explores on weeknights, inside the agent
jail (`../agent-jail/README.md`). It uploads no artifacts and logs no
findings, because the repo is public: screenshots and detail go to Linear
only.

The local stack still works for trying out prompt changes:

```bash
mise run magellan:explore --since "3 days ago" --budget 20
mise run magellan:file                 # dry run: prints the issues
mise run magellan:file --file          # needs LINEAR_API_KEY
```

## The bot user

Production login goes through WorkOS and cannot be scripted. Magellan instead
reuses a real session. A `gram_session` stays valid for as long as it is used
at least once every 72 hours, and the workflow's daily `keepalive` job uses it.

To set it up, or to replace a dead session:

1. Create a bot user that is a member of the integration org only, with no
   MFA and the smallest role that can create MCP servers and assistants.
2. Log in as the bot in a fresh browser profile. In devtools, copy the
   `gram_session` cookie value.
3. Close that profile **without logging out**. Logging out ends the session.
4. In the repo's `magellan` environment, set the `MAGELLAN_SESSION` secret to
   the cookie value, the `MAGELLAN_ORG` variable to the integration org's
   slug, and the `ANTHROPIC_API_KEY` and `MAGELLAN_LINEAR_API_KEY` secrets.
   If the dashboard loads from hosts besides the site, list them in the
   `MAGELLAN_EXTRA_HOSTS` variable (comma-separated); everything else is
   blocked.
5. Under the environment's **Deployment branches**, allow `main` only.
   Otherwise anyone who can push a branch could run a changed workflow with
   these secrets.

The site is fixed in the workflow and in `explore.sh`
(`https://app.getgram.ai`), not a variable: the session is never sent
anywhere else. Change both, in a reviewed PR, if the dashboard moves.

The session dies on logout-everywhere, on removal from the org, or when
session storage is reset. When it dies, `keepalive` fails with a message
pointing here.

The agent's rules for a shared org: it names what it creates
`Magellan <run id> ...`, edits or deletes only data with its own run's id,
deletes that data before it finishes (it keeps the last 10 minutes of its
budget for this), never changes org settings, and never sends anything
outside the product. What a run could not delete is listed in its
`notes.md`; nothing deletes another run's leftovers by name, so clear those
by hand.

## Security

- **The agent holds no secret.** The jail loads the bot's session into the
  browser and deletes the file before the agent starts. The Anthropic key
  stays with the jail's proxy. The Linear key exists only in the filing
  step, after the agent has exited.
- **Its browser commands are an allowlist** (`browser.mjs`): no `eval`,
  cookies, storage, headers, uploads or route mocks, and no navigation off
  the site.
- **Its network is the site and the Anthropic API**, enforced by the jail,
  not by the prompt.
- **Its output is untrusted.** `magellan:file` validates each finding, puts
  agent text into Linear only inside code blocks, and in CI logs nothing the
  agent wrote.

## Not yet

Magellan files issues only. Opening draft fix PRs comes once its findings
have proven trustworthy for a few weeks.
