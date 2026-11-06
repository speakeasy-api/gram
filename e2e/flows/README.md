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

CI runs `.github/workflows/flow-checks.md`, a
[GitHub Agentic Workflow](https://github.github.com/gh-aw/) compiled to
`flow-checks.lock.yml` with `gh aw compile flow-checks`:

1. **Replay** — ordinary workflow steps boot the stack and run
   `mise run test:flows`. No LLM. If every flow passes, the run stops there.
2. **Heal** — otherwise a Claude Code agent re-walks each failing story in a
   real browser (`.github/prompts/heal-flow.md`) and returns one verdict:
   - `healed` — the flow still works; the UI moved. It rewrites
     `flow.spec.ts` until the replay passes.
   - `broken` — a user can no longer finish the flow.
   - `changed` — the diff intentionally changes the flow. The story needs a
     human edit; the agent proposes one.
3. **Report** — one PR comment with the verdicts and the commands to replay
   and heal each failing flow on your machine. Healed scripts are in the
   run's `healed-flows` artifact; review one before you commit it. The check
   does not block merges yet.

The agent runs in gh-aw's AWF sandbox: a container whose network is limited
to the stack's dashboard, server and dev-idp ports on the runner, and whose
Anthropic key is held by AWF's API proxy, not the agent. It writes nothing
back except through gh-aw's safe outputs (the comment and the artifact).
`ATLAS_TOKEN` exists only in the step that boots the stack.

Locally, `mise run flows:heal <flow>` runs the same prompt against your own
stack, with no sandbox: the agent runs as you.

`mise run test:agent-checks` tests all of this without a model, a browser or
Linear: the workflow's sandbox, secret and safe-output invariants, the replay
step's parsing of Playwright results, the local heal and Magellan tasks (with
`claude` stubbed), and the Magellan filer (with Linear mocked). CI runs it as
`agent-checks-test` in `pr.yaml`.

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
