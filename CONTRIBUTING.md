# Development setup

Run `./zero` until it succeeds. This script is what you will use to run the dashboard and services for local development. It will also handle installing dependencies and running pending database migrations before starting everything up.

The main dependencies for this project are Mise and Docker. The `./zero` script will guide you to install these if they are not found.

### Seeding the database

Once `./zero` succeeds and all services are running, seed the local database with sample data:

```bash
mise seed
```

This populates the database with projects, deployments, API keys, and other resources so you have something to work with in the dashboard. The seed script authenticates against the running server, so make sure the services are up before running it.

### Local auth and identity (dev-idp)

In production, Gram relies on two external services for identity:

1. **An OIDC provider** — handles the login flow (redirect → authenticate → callback with authorization code → exchange for tokens).
2. **WorkOS** — provides organization management, role-based membership, and team invitations. The Gram server calls the WorkOS REST API directly for things like listing roles on the Roles & Permissions page, managing org memberships, and syncing user data.

For local development, both of these are replaced by **dev-idp** — a lightweight Go server in the `dev-idp/` directory that runs as a process in madprocs. It uses SQLite for storage and requires no external accounts or API keys.

dev-idp mounts several protocol handlers, all sharing the same SQLite database:

| Prefix         | Always mounted              | Purpose                 | Details                                                                                                                                                                                  |
| -------------- | --------------------------- | ----------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `/oauth2`      | ✅                          | User login              | Standard OIDC provider (authorize, token, userinfo, JWKS). When you click "Login" in the dashboard, the browser redirects here, dev-idp issues tokens, and the server creates a session. |
| `/oauth2-1`    | ✅                          | MCP auth                | OAuth 2.1 server with PKCE (S256) and Dynamic Client Registration. Used by MCP remote session flows. Shares the same user/org data as `/oauth2`.                                         |
| `/mock-workos` | ✅                          | Orgs, roles, teams      | Mock WorkOS REST API. Dashboard pages that call WorkOS directly (Roles & Permissions, Team management, Invitations, membership checks) hit this instead of the real API.                 |
| `/workos`      | ❌ (needs `WORKOS_API_KEY`) | Real orgs, roles, teams | Thin proxy to the real WorkOS API. Forwards requests using your API key. Replaces `/mock-workos` when you need real data.                                                                |

The Gram server doesn't know or care that these are mocked — it uses the same code paths as production. The only difference is that `GRAM_IDP_BASE_URL` and `WORKOS_API_URL` point to `localhost` instead of external services.

#### Mode: mock-workos (default — zero config)

This is what you get out of the box after running `./zero`. No configuration needed.

- **Login**: Clicking "Login" authenticates instantly with a test user. No credentials needed.
- **WorkOS features**: Dashboard pages that call WorkOS (Roles & Permissions, Team management, Invitations) work against the mock, returning synthetic data.
- **Customization**: Open the dev-idp dashboard at `http://localhost:35293` to create/edit users, organizations, roles, and memberships. Changes take effect on next login.

#### Mode: real WorkOS (opt-in)

For Speakeasy employees who want to test against real WorkOS data (real organizations, real roles, real AuthKit login). The `./zero` script prompts you to choose during setup, or you can switch manually.

Add the following to `mise.local.toml`:

```toml
[env]
WORKOS_API_KEY = "sk_test_..."
WORKOS_API_URL = "{{env.GRAM_DEVIDP_EXTERNAL_URL}}/workos"
GRAM_IDP_CLIENT_ID = "client_..."
```

This does two things:

1. **dev-idp mounts `/workos/`** — a thin proxy that forwards API requests to the real WorkOS API using your API key. The Gram server's WorkOS client calls go through this proxy.
2. **The OIDC login still goes through dev-idp's `/oauth2`** — dev-idp handles the token exchange locally.

After changing modes, restart madprocs for changes to take effect.

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
