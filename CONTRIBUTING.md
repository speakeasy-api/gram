# Development setup

Run `./zero` until it succeeds. This script is what you will use to run the dashboard and services for local development. It will also handle installing dependencies and running pending database migrations before starting everything up.

The main dependencies for this project are Mise and Docker. The `./zero` script will guide you to install these if they are not found.

### Seeding the database

Once `./zero` succeeds and all services are running, seed the local database with sample data:

```bash
mise seed
```

This fills your organization with a realistic working environment: a project with a deployed API and toolsets, ~180 agent sessions with full transcripts and cost telemetry, risk findings and policies, teammates, budgets, skills, and an API key. It also makes you an admin of that org.

It writes directly to Postgres and ClickHouse, so it needs the infra containers but not the server — you can run it before or after `mise run start`, and re-run it any time. It is idempotent: every run wipes the seeded data and lays it down fresh, so it is also how you reset a database you have made a mess of.

The data is the same data the public demo organization is built from, retargeted at your local org. See [seed/demo/README.md](./seed/demo/README.md) for how that works and what is local-only.

### Local auth and identity (dev-idp)

Local development uses **dev-idp** — a lightweight Go server (`dev-idp/`) that replaces the external identity services Gram uses in production. It runs as a pitchfork daemon, uses SQLite, and requires no external accounts.

Whichever backend you pick, **login is non-interactive**: you click "Login" and are signed straight in, with no credentials and no hosted login page. That is the point of dev-idp. What the backend changes is only where your identity comes from.

```
GRAM_DEVIDP_BACKEND = "local" | "workos"
```

#### local (default — zero config)

This is what you get out of the box after `./zero`. No configuration needed.

- **Login** signs you in as your git committer identity (`git config user.email`), synthesized on first use.
- **WorkOS API calls** (Roles & Permissions, Team management, Invitations) hit dev-idp's built-in emulator instead of the real API.
- **Customization**: open `http://localhost:35293` to create/edit users, organizations, roles, and memberships.

#### workos (opt-in — real WorkOS)

For Speakeasy employees testing against real WorkOS data. The `./zero` script prompts you to choose during setup, or add to `mise.local.toml` manually:

```toml
[env]
GRAM_DEVIDP_BACKEND = "workos"
GRAM_IDP_CLIENT_SECRET = "sk_test_..."
```

That is the whole configuration — no URLs to repoint. dev-idp passes REST calls through to the real WorkOS API, so roles, memberships and invitations act on live data.

Login stays non-interactive: dev-idp looks your git committer email up **through** the WorkOS API and signs you in as that WorkOS user, reporting its real user id. So you need a WorkOS user whose email matches `git config user.email`.

> [!NOTE]
> Don't set `GRAM_IDP_CLIENT_ID` to a real `client_...` value. The server sends any `client_`-prefixed id to WorkOS's hosted AuthKit, which is an interactive login — the opposite of what dev-idp is for.

After changing backends, restart pitchfork with `mise run start`. Each backend keeps its own `current_users` row, so switching back and forth preserves whichever identity you had set on the other side.

dev-idp brings its own SQLite database up to the current schema on start, so pulling a schema change does not require wiping it — existing users, organizations and memberships are kept. If a change ever needs more than SQLite can do in place, it says so on boot and names the column.

<details>
<summary>dev-idp protocol handlers (advanced)</summary>

dev-idp mounts two surfaces on a single port, sharing one SQLite database:

| Prefix      | Purpose            | Details                                                                                                               |
| ----------- | ------------------ | --------------------------------------------------------------------------------------------------------------------- |
| `/oauth2-1` | Login + MCP auth   | OAuth 2.1 authorization server. PKCE and Dynamic Client Registration; both optional for the first-party login client. |
| `/workos`   | Orgs, roles, teams | WorkOS REST surface. Emulated locally or proxied upstream, per `GRAM_DEVIDP_BACKEND`.                                 |

Login spans both: the browser hits `/oauth2-1/authorize`, which redirects straight back with a code, and the server exchanges that code at `/workos/user_management/authenticate` rather than the OAuth token endpoint — it drives login through the WorkOS user-management SDK. `authenticate` is therefore always served locally, even under the `workos` backend, because real WorkOS would reject a code dev-idp minted.

The Gram server uses the same code paths as production — only `GRAM_IDP_BASE_URL` and `WORKOS_API_URL` point to localhost instead of external services.

</details>

### Parallel worktrees

Each git worktree can run its own full stack — server, worker, dashboard, databases — side by side with the others. Ports and the Docker Compose project name are remapped per worktree, so nothing collides with your primary checkout.

The flow is driven by [worktrunk](https://worktrunk.dev), configured in `.config/wt.toml`:

```bash
wt switch --create my-feature   # create the worktree and boot its stack
wt status                       # which stacks are up, and on which URL
wt remove                       # tear the stack down and delete the worktree
```

Creating a worktree runs two hooks:

- **`pre-start`** runs `mise run git:workinit`, which copies `mise.local.toml` and `local/` from the primary worktree, installs dependencies, and assigns this worktree a free port for every `*_PORT` variable plus its own `COMPOSE_PROJECT_NAME`.
- **`post-start`** runs `./zero --agent` in the background to bring up infra, apply migrations, start the daemons, and seed.

> [!NOTE]
> `INFRA_READINESS_TIMEOUT` is raised for the `post-start` hook. A new worktree always starts from a cold volume, so Postgres has to finish `initdb` before it accepts queries — well past the 30s default that `mise infra:start` uses elsewhere.

Because `post-start` is backgrounded, `wt switch` returns before the stack is ready — expect a couple of minutes. Use `wt status` to see when it comes up, and `wt config state logs` if it doesn't:

```
  Branch          State  URL
@ main            ● up   https://localhost:5173
+ my-feature      ● up   https://localhost:52495
+ old-experiment  ○ down https://localhost:62875
```

`wt remove` runs `mise run nuke --keep-shared` inside the worktree first, so the right Compose stack is torn down before the directory disappears (the shared Presidio stack stays up for other worktrees). Removing a worktree by hand (`git worktree remove`) skips that and leaves its containers and volumes running.

#### Recommended shell setup

Worktrunk needs a shell wrapper to change directory on your behalf. Install it once:

```bash
wt config shell install
```

If you already have aliases for the branch commands this flow replaces, pointing them at `wt` makes worktrees the default without learning new muscle memory:

```bash
alias nb='wt switch --create'   # new branch, in its own worktree
alias gco='wt switch'           # switch; no argument opens a picker
alias gbd='wt remove'           # tear down the stack, remove the worktree
alias wts='wt status'           # which stacks are up
```

`wt switch` also takes shortcuts in place of a branch name: `^` for the default branch, `-` for the previous worktree, and `pr:123` to check out a pull request. Prefer `^` over hardcoding `main` — it resolves correctly in any repo.

### CLI development

Quickstart:

```bash
cd cli
go run . --help
```

# Contribution guidelines

Above anything else in this document: we do not perfectly hold to the guidelines below but we do our best to work towards them. Active codebases will readily deteriorate with time unless explicit efforts are made to reverse deterioration.

Good and bad decisions compound and the goal of this document is to get you making good decisions that help build Gram up as a useful and high-quality product.

## Preamble

<details open>
<summary>Why do we even have this document?</summary>

**The world is full of APIs that we want AI agents to leverage effectively.**

Gram is an exploration into how we can take that vast space of APIs and create the right bridges to them for AI agents. We welcome ideas as much as code contributions that serve this goal.

**Open source as a team effort.**

The goal of open sourcing Gram is to recognize that we solve problems better as a community rather than as a walled off teams and give confidence to those that want to use the service either through Speakeasy or self-hosted. For Gram to succeed as useful product, we want to welcome contributors that share our values and goals around building high-quality products for the Agentic AI frontier.

**High quality products are built from high quality decisions.**

The goal of these guidelines and any roadmap plans made in Gram is to ensure we are solving the right problems to connect AI agents to the sprawl of APIs in the world. This may mean we choose to work on some things over others or reject proposals that we do not believe serve this goal. We encourage productive discussions and opinions that are grounded in research and ultimately lead us to make good decisions when building Gram.

</details>

## General practices

<details open>
<summary>High-level behaviors we're looking for</summary>

**Readability, maintainability, strong conventions and the long view.**

We want to be fast at every stage of developing Gram. We're not going to over-index on throwing code into production with no checks and balances when it means we'll sink into a tarpit of bugs and incidents months from now. There is a widely-applicable definition of "fast iteration speed" that includes not making messes along the way. Establishing guardrails and conventions in coding and codebase structure means we can increase our iteration speed by adding well-aligned contributors. Everyone will benefit from this: current and future users and contributors.

**You are the first reviewer of your AI assistant's contributions.**

You are responsible for all the work that you and your assistants produce. You must be the first reviewer of all your work before requesting reviews from anyone else.

**Add tests for all new contributions.**

Coding agents and assistants are fantastically effective at this. Utilize them if you can but always review their work to ensure that they are indeed testing the changes you/they make. The goal is not to hit 100% test coverage but to have higher and higher confidence that the code you write does what you expect it to do and enable others to maintain it well.

**Add documentation whenever possible.**

We document how we deploy Gram, how we manage the database schema and migrations and how we manage infrastructure. A lot of this documentation should act as a sort of runbook that aids new contributors and incident responders. If you are materially affecting any of these areas, please add documentation to help others understand how to operate Gram.

**Code comments are great.**

We are not prescriptive about code comments but we encourage them. Particularly on methods and exported types since these greatly help coding assistants understand the codebase without having to always to consider all logic.

**Code reviews are great.**

We review all contributions to Gram and will favor pull requests that are small and focused over massive and far reaching ones. We have no preference or expectations for how you structure your commits since we squash all commits on merge. We do however appreciate contributors that spend any time to structure their work if size of change is large.

Above all, we expect folks to spend non-zero effort adding a meaningful pull request title and description since these will contribute to the changelog.

**Too much nesting kills readability.**

Our brains are very slow code interpreters. We can help them along by managing code complexity to optimize for readability. _Functions that have 4 or more levels of nested code where branches have substantial amounts of business logic are heavily discouraged._ The contrived example below is the upper bound of what we consider comprehensible code:

```go
func doSomething() error {
	for event := range events {
		switch event.Type {
			case EventTypeA:
				val, err := lookupDatabase(event.ID)
				if err != nil {
					return fmt.Errorf("lookup event: %w", err)
				}

				res, err := callAPI(val.URL)
				var tempErr *TemporaryError
				if errors.As(err, &tempErr) {
					continue
				} else if err != nil {
					return fmt.Errorf("call api: %w", err)
				}
			case EventTypeB:
				// ...
			case EventTypeC:
				// ...
			default:
				// ...
		}
	}

	return nil
}
```

Note that trivial `if err != nil` branches are discounted here.

_We **do not** subscribe to concepts like cyclomatic complexity or lines of code, only a simple metric of how nested is business logic in a region of code._ For non-trivial changes and additions, review your code and consider if it can be broken up to promote a [line-of-sight][los] and in turn improve readability. To reiterate: long functions are usually fine, wide functions are often not. As with most/all rules, there are certainly exceptions to this rule but they will be very rare.

[los]: https://medium.com/@matryer/line-of-sight-in-code-186dd7cdea88

</details>

## Releases

> [!NOTE]  
> All CLI updates must follow the [changeset process](./docs/runbooks/version-management-with-changesets.md).

New versions of the CLI are released automatically with [GoReleaser](./.goreleaser.yaml).

Version bumps are determined by the git commit's prefix:

| Prefix   | Version bump | Example commit message                  |
| -------- | ------------ | --------------------------------------- |
| `feat!:` | Major        | `feat!: breaking change to deployments` |
| `feat:`  | Minor        | `feat: new status fields`               |
| `fix:`   | Patch        | `patch: update help docs`               |
