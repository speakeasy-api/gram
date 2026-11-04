# Authorization end-to-end suite

Proves who may and may not do what, against a running local stack. It exists so
that a change to how authorization is evaluated can show, with one command, that
every outcome a user or API key sees is unchanged, or exactly which ones moved.

```bash
./zero --agent             # once, on a fresh checkout: boot and seed the stack
mise run test:rbac-e2e     # the whole suite
mise run test:rbac-e2e --project=contract   # API layers only (about 30 s)
mise run test:rbac-e2e --project=ui         # dashboard journeys only
mise run test:rbac-e2e --grep "projects"    # one slice
```

The HTML report lands in `e2e/.playwright/rbac-report`.

## Who the tests act as

The global setup (`global-setup.ts`, `lib/personas.ts`) logs each user persona
in through the real OAuth flow of the local identity provider, assigns its role
through `access.updateMemberRoles` as the seeded organization admin, logs it in
again so its stored session postdates the assignment, and mints the two API
keys. Nothing uses the `X-Gram-Scope-Override` dev header, because that
would skip the principal and role resolution an authorization change touches.

| Persona                      | What it holds                                                                                                      |
| ---------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| `admin`                      | built-in Admin role                                                                                                |
| `member`                     | built-in Member role                                                                                               |
| `reader`                     | custom role: `org:read`, `project:read`                                                                            |
| `blocked`                    | custom role: project and MCP read/write on every project, plus `project:blocked_write` on the default project only |
| `elsewhere`                  | `project:read` everywhere, MCP read/write/connect only on the sandbox project                                      |
| `norole`                     | organization member with no role                                                                                   |
| `outsider`                   | user whose only organization is a different one                                                                    |
| `consumerKey`, `producerKey` | organization API keys with those key scopes                                                                        |
| `anonymous`                  | no credential                                                                                                      |

Users are `<persona>@rbac-e2e.local`; roles and the sandbox project are prefixed
`rbac-e2e`. Setup is idempotent, so the suite can run repeatedly on one stack.

## What it covers

**Read sweep** (`contract/read-sweep.spec.ts`). Every parameter-free `GET`
endpoint (193 of them, listed in `read-sweep.endpoints.ts`) called as every
persona, compared with `read-sweep.baseline.json`. The baseline stores exact
statuses; a run compares outcome classes (reached the handler, 401, 403, 404,
5xx), because a few handlers answer 200 or 400 depending on background work the
stack may not have finished.

**Contract table** (`contract/cases.ts`). Mutations and situations the sweep
cannot reach: organization invites and API keys, project create/update/delete
including a block (`project:blocked_write` refuses the default project but not
the sandbox), toolset create/read/update/clone/delete and MCP server create/update including
key scopes, an MCP client's `tools/list` and `tools/call` through the gateway,
assistant create/read/update/delete and execution profiles, agent
registration, role creation and role reassignment. Each row
lists the status each persona it covers must get (personas that cannot use an
interface, such as sessions on the MCP gateway, are left out of some rows); rows create their own fixtures and
clean up after themselves.

**Dashboard page gates** (`ui/page-gates.spec.ts`). Organization admin pages,
project overview and settings (with the block), MCP servers, assistants and the
roles page, each opened as the personas allowed and denied it.

**Dashboard journeys** (`ui/journeys.spec.ts`). Login through the dashboard
button; a role change taking effect in the dashboard; the blocked persona's
project settings Save disabled on the blocked project and succeeding on the
other; an admin creating an API key from the dashboard.

The suite runs in the order of the authorization migration: organization,
projects, MCP, agents and assistants, access administration.

## Adding coverage

- An API endpoint with no required parameters: append it to
  `read-sweep.endpoints.ts` and re-record (below).
- Anything else on the API: add a row to `CASES` in `contract/cases.ts`. The spec
  never needs to change.
- A dashboard page: add a row to `GATES` in `ui/page-gates.spec.ts`.

## When an outcome is meant to change

Re-record the sweep and review the diff:

```bash
RBAC_E2E_UPDATE_BASELINE=1 mise run test:rbac-e2e --project=contract
git diff e2e/rbac/contract/read-sweep.baseline.json
```

For contract and page-gate rows, edit the expected status. Either way, say in
the pull request which outcomes moved and why. An unexplained diff in these
files is the signal this suite exists to raise.

## Behaviour pinned that may deserve a second look

These are today's outcomes on `main`, recorded as they are. They are not
endorsements.

- The project settings page opens for the `blocked` persona on the project its
  role blocks writes to; only the Save button is disabled. The page-level gate
  looks at `project:write` without the block, the component-level gate applies
  it.
- A project-scoped call from someone with no grant on the project (`norole`,
  `outsider`) answers 401 rather than 403, because it fails while the
  `Gram-Project` header is resolved.
- `projects.get` for a project the caller cannot see answers 404, which hides
  that it exists.
- The `elsewhere` persona, whose MCP grants are bounded to the sandbox
  project, can clone toolsets in the default project and list its MCP
  endpoints, meta, remote and unproxied servers (`toolsets.clone`,
  `mcpEndpoints.list`, `metaMcp.list`, `remoteMcp.listServers`,
  `unproxiedMcp.listServers`), and so opens the default project's MCP page.
  Those checks name no project. Every other MCP row denies it.
- Any organization member can register an agent they own; `agents.create` is
  allowed for every user persona.
- Several organization endpoints answer 403 even to `admin`
  (`externalCredentials.*`, `externalKeys.*`, `jsonWebKeySets.list` and others in
  the baseline). These are staff or feature-gated surfaces.

## CI

`.github/workflows/rbac-e2e.yml` boots the stack with `./zero --agent` and runs
`mise run test:rbac-e2e`. It runs on pull requests that touch authorization code
or this suite, on any pull request labelled `rbac-migration` whatever its base
branch, and by `workflow_dispatch` with an optional `ref`. The Playwright report
is uploaded as the `rbac-e2e-playwright` artifact; CI records no traces, since
they would carry persona sessions into a downloadable file.

## Environment

The suite reads `GRAM_SERVER_URL`, `GRAM_SITE_URL` and
`GRAM_DEVIDP_EXTERNAL_URL` from the worktree's mise env, and needs
`GRAM_DEVIDP_BACKEND=local` (the default). Optional:

- `RBAC_E2E_PROJECT` – project slug the personas are evaluated against
  (default `default`).
- `RBAC_E2E_CHROMIUM_PATH` – a Chromium binary to use when the machine's
  preinstalled browser is not the build the pinned Playwright expects.
- `RBAC_E2E_UPDATE_BASELINE=1` – re-record the read sweep.

Agents have no delete endpoint, so every run leaves a few `rbac-e2e-agent-*`
agents behind. Everything else the suite creates is removed.
