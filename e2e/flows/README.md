# Flow checks

Flow checks answer one question about a change: **can a user still finish
flow X?** They watch for UX drift — a change that silently leaves a product
flow incomplete — not for pixel changes.

Each flow is a directory:

```
e2e/flows/<flow>/
  story.md        # the source of truth, written by people
  flow.spec.ts    # a Playwright script an agent compiled from story.md
```

## The story

`story.md` describes the flow as user intent, not as selectors. An agent reads
it to write the script and, later, to judge whether a failing script means the
UI moved or the flow broke. Keep it in this shape:

```markdown
# <Flow name>

<One sentence: why a user does this.>

## Given

- <State the seed provides before the flow starts.>

## When

1. <A step, in the words a user would use.>
2. ...

## Then

- <An outcome the script must prove with a hard check: an RPC response,
  a row in a list endpoint, a URL. Never "the page looks right".>
```

Steps describe goals ("add Slack from the catalog"), so a renamed button does
not change the story. A removed step or a new required step does.

## The loop

1. **Replay** — `mise run test:flows` runs every `flow.spec.ts` against the
   local stack. It needs no LLM of its own, so it is cheap. CI runs this on
   PRs that touch the dashboard.
2. **Heal** — when a replay fails, `mise run flows:heal <flow>` hands the story,
   the failure, and the branch diff to a Claude Code agent. The agent walks the
   story in a real browser and returns one verdict:
   - `healed` — the flow still works; the UI moved. It rewrites
     `flow.spec.ts` and the replay passes again.
   - `broken` — a user can no longer finish the flow. It names the step that
     fails and leaves a screenshot.
   - `changed` — the diff intentionally changes the flow. The story needs a
     human edit; the agent proposes one.
3. **Report** — CI posts the verdicts as one PR comment, with the commands to
   replay and heal each failing flow on your machine. A healed script is in
   the run's `flow-checks` artifact under `healed/<flow>/`; review it before
   you commit it. The check does not block merges yet.

## Healing in CI

CI heals inside the agent jail (`../agent-jail/README.md`): the agent sees a
copy of `e2e/` and the branch diff, reaches only the local stack and the
Anthropic proxy, and holds no secret. The job that boots the stack drops
`ATLAS_TOKEN` after boot and leaves no GitHub token on disk, and the PR
comment is posted by a separate job that runs no repository code. In the jail
the agent uses `flow-browser`, `flow-test` and `flow-typecheck`
(`jail-bin/`) instead of the `mise` tasks.

Locally, `mise run flows:heal` runs the same prompt (`HEAL.md`) without a
jail, against your own stack.

## Writing a new flow

1. Write `story.md`.
2. Run `mise run flows:heal <flow>`. With no `flow.spec.ts` yet, the agent
   compiles one from the story.
3. Read the generated script. Commit both files.

Scripts must create their own data with a unique suffix (`runId()` in
`fixtures.ts`), so a rerun never collides with an earlier run.

## Starter flows and their dependencies

Not every flow is equally repeatable:

| Flow                 | Depends on                                         | Repeatable                                 |
| -------------------- | -------------------------------------------------- | ------------------------------------------ |
| `server-from-source` | the local stack only                               | yes                                        |
| `assistant-create`   | an LLM (`OPENROUTER_DEV_KEY`); skipped without one | no: the builder's replies vary             |
| `catalog-connect`    | the live MCP registry and DeepWiki's remote server | no: an outage or a catalog change fails it |

A failure in a flow that is not repeatable is not proof of a regression on
its own. The heal agent re-walks the story and says which it was, and the
check does not block merges.
