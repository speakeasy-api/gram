# Description

We are using this worktree to create a complicated design document — an RFC titled "Remote OAuth Clients for Private Repos". It has several goals:

1. Enable Gram client sessions to connect to remote OAuth provider sessions even when those sessions must acquire a token for an external server. **This is THE fundamental product goal**, though we are bundling in a number of architectural improvements to our OAuth pathways. Currently, the same Gram component plays both roles: AS to the MCP client _and_ OAuth client to the remote. These responsibilities must be split.

2. Decouple client sessions from remote sessions at the data-model layer. That is: sessions established between Gram and MCP clients are distinct from sessions between Gram and remote OAuth providers.
   - **Client sessions** = signed Gram access tokens (JWT) plus refresh tokens stored in Redis attached to the session.
   - **Remote sessions** = stored in Redis on separate documents, keyed on `(session_id, client_session_issuer_id)` rather than the access token value. Each document holds the access token and the refresh token in a structure that lets them have entirely separate TTLs.

3. Remove deprecated functionality we encounter along the way. Examples: storing OAuth secrets on an `oauth_proxy_provider` record (`oauth_proxy_providers.secrets`, `security_key_names`, `provider_type='custom'`, etc.). The full list will emerge as we design.

4. Remove a particular security vulnerability from Gram OAuth: today, consent can be bypassed when a session already exists. The `/authorize` endpoint must verify that the user has previously consented for this `client_session_issuer` to access **all** of its `remote_session_tokens`. Only on a positive match may consent be skipped.

5. Decouple the concept of `oauth_providers` into `remote_oauth_issuer` and `remote_oauth_client`. This gives us:
   - the ability to manage the lifecycle of automatically-discovered remote issuers
   - the ability to automatically create remote clients
   - schema-level support for many `remote_oauth_clients` per `remote_oauth_issuer` (so that, in the future, multiple credentials can fan out to a single shared remote — e.g. multiple Notion app credentials against a single Notion MCP). For the scope of this work, the relationship will be 1:1 in practice.

6. Add extensive integration test coverage for all combinations of Gram sessions and sources, especially the complicated configurations we see in production today.

7. Lift token-exchange concerns out of `doHTTP` in the tool proxy and homogenize remote-server token provisioning into a layer wholly decoupled from tool execution.

8. Consider where OIDC-awareness should enter the system. Each `remote_oauth_issuer` has a required `oidc` boolean column, defaulted to `false`. Setting it `true` may unlock new behaviors.

9. There is no longer any directory just called `oauth`. Every directory must be qualified by what kind of OAuth concern it carries: client-session, remote-session, or some other concept entirely. OAuth is not a single concept in our system — we treat authentication and authorization in many ways and should stop mushing them together.

10. Use the chat-sessions JWT manager to sign and issue client-session access tokens, with the authenticated identity embedded as the JWT `sub`.
    - We add a new `PrincipalType` (joining the existing `user` and `role`) called `anonymous`. URNs follow the existing 2-segment shape — `anonymous:<mcp-session-id>`.
    - The set of URN shapes valid in JWT `sub` is: `user:<id>`, `apikey:<uuid>`, `anonymous:<mcp-session-id>`. **`role:<slug>` is NOT valid in `sub`** — roles are not authentication subjects.
    - `urn.APIKey` remains a parallel URN kind (it is not a `PrincipalType`); we are deliberately keeping that separation but allowing both PrincipalType and APIKey URNs to share the same `sub` claim.
    - Anonymous principals can **only** be provisioned through the MCP pathway.
    - The JWT _schema_ conforms to OIDC (i.e. `sub`, `aud`, `iss`, `iat`, `exp`, etc.). We **do not** adopt OIDC's mandated public-key signing — we keep our existing HS256 / `GRAM_JWT_SIGNING_KEY` setup.
    - `aud` for client-session tokens is the toolset slug. This lets validators reject chat-session JWTs presented as client-session tokens and vice versa.
    - When we document the JWT schema in `/schemas/`, we will clearly document every URN.

11. The `client_session_issuer` package never generates its own session IDs. Every method that creates a session must **accept** a session ID externally. When consumed from the MCP handler, the injected value is the `mcp_session_id`. The package itself stays agnostic about where the ID comes from — this preserves a clean seam for non-MCP consumers in the future and keeps the MCP/session-ID coupling at the call site, not in the package.

## Out of scope

- OAuth sessions in the Gram playground. The `user_oauth_tokens` table and the playground UX flows are **entirely unrelated** to this work — leave them alone.

## Philosophically

- This work should be decoupled into as many independent, loosely-coupled tidbits as possible. We will build a highly-idealized version of the flow on this branch and then, for each possible increment, either (a) evaluate whether it can be implemented now or (b) instrument the code so we have enough information to evaluate it later. Nothing is preserved by default — every existing feature is on the table for removal.

# Context

Skills (these don't exist yet — we will create them as ongoing documentation for agents):

- **Gram** — what is it? How does it work? How does its MCP pathway work?
- **Gram Legacy OAuth** — what is it? Why does it work the way it does? What are the principles behind where we're migrating to?
- **MCP Servers and Endpoints** — internal-only Gram concept (`mcp_servers` + `mcp_endpoints`, formerly `mcp_frontends` + `mcp_slugs`). Pinned references:
  - https://www.notion.so/RFC-Gram-MCP-Frontends-and-Slugs-342726c497cc800ba609de5cbe5f3d38?source=copy_link
  - https://www.notion.so/speakeasyapi/RFC-Gram-Remote-MCP-Servers-33c726c497cc8072ac6dc6816f3d264f?source=copy_link
  - https://github.com/speakeasy-api/gram/pull/2412
- **MCP specification** — handshakes (SSE), OAuth discovery, MCP registries. Should not be redundant with general knowledge.
- **OAuth** — links to authoritative spec material rather than re-explaining it.

# Structure

We are going to maintain several artifacts in this repo:

- **`spike.md`**: generalized design documentation. Includes:
  - description of the spike
  - renderings of the mermaid diagrams for each key flow
  - references to the SDL of the schema for the project
  - reference documentation for all management endpoints for the OAuth authorization servers
  - **definitions section up front** — first general OAuth terms, then _our_ terms. Notable ones:
    - `client_session_issuer`
    - `remote_oauth_issuer`
    - `remote_oauth_client`
    - **passthrough mode** (in the context of `remote_oauth_issuer`): proxy the bearer the MCP client sent us. We will conform to the existing abstractions even if that means storing the token — we care about homogeneity, not about avoiding storage as such.
    - **implicit vs interactive modes** for `client_session_issuer`. There can be multiple `remote_oauth_issuers` per `client_session_issuer`. After issuing a client session, Gram completes the OAuth challenges mandated by each remote issuer.
      - **implicit**: redirect through each subsequent challenge from our callback, build the entire session, then redirect back to the client callback for the final token exchange. Consent must still be prompted somewhere in the request stream — there is just no intermediate "click each server" UI.
      - **interactive**: issue a client session, then render a UX where the user clicks each remote OAuth server to authenticate.

- **`project.md`**: tickets defining the scope of work. Tracks all follow-on PRs and milestones. Milestones (roughly):
  - **Milestone #0 — Instrumentation**: add instrumentation to existing OAuth flows so we know which existing functionality to sunset. Tickets include: knowing when we use passthrough vs. authenticated vs. anonymous sessions, decorating the logger with MCP session ID, adding logging to all chat-session-ID usage so we know how to keep chat-session-ID issuing backwards-compatible.
  - **Milestone #0b — Mock IDP upgrade**: upgrade the mock IDP to run in many modes. Paths at minimum: `/workos/`, `/mock-speakeasy/`, `/oauth2dot1/`, `/oauth2/`. Backed by the same SQLite database of applications. Runnable as its own binary alongside `cmd/server` and `cmd/worker`. Serves a small static page; we'll add a new project directory `mock-idp-ui` — a Hono package using `@hono/react-renderer` (or equivalent client-side React integration) — for the page itself.
  - **Milestone #1**: instrument `x/mcp` with client sessions in three modes — authenticated, anonymous, passthrough.
  - **Milestone #1.5**: add management APIs for the new OAuth schemas.
  - **Milestone #2**: instrument `x/mcp` with passthrough authentication. (Same concept as `passthrough mode` in the definitions.)
  - **Milestone #3**: instrument `x/mcp` with remote sessions in **implicit** challenge mode (no intermediate UI redirect chain — but consent must still be prompted somewhere).
  - **Milestone #4**: instrument `x/mcp` with remote sessions in **interactive** ("multi-plexing") mode.
  - **Milestone #5**: add migration to `toolsets` so they support setting a `client_session_issuer` in addition to the two legacy OAuth modes.
  - **Milestone #6**: migrate all servers on the current `external_oauth_provider` model to `client_session_issuer` with **passthrough mode**.
  - **Milestone #7**: migrate all servers on the current `oauth_proxy_server` model to the new `client_session_issuer`. This requires porting `oauth_proxy_providers` → `client_session_issuers`.
  - **Milestone #8**: add a mode that requests stale remote-session information through URL-mode elicitation. The URL goes to the same challenge screen as `interactive` mode.

- **`/diagrams/`**: a directory with one mermaid diagram per pertinent flow. Should include:
  - `mcp-handler.mermaid`
  - `client-session-challenge.mermaid`
  - `remote-session-challenge.mermaid`
  - `unified-challenge.mermaid`
    Do **not** fill any of these in yet.

- **`/schemas/`**: schemas for the upgraded implementation. Postgres in Postgres SDL; Redis JSON in Go SDL. We have started these artifacts in `jwt-schema.go`, `oauth-schema.sql`, `redis-oauth-schema.go`.

- **The branch itself** serves as a reference implementation (NOT TO BE MERGED), used to plan the delta in detail. Each time we remove schema from the database we will ensure there is a ticket tracking its removal and we'll track any necessary dependencies needed to push those changes forward.

# Process

ANY TIME we identify code that can or should be deleted, we will: delete it, create a ticket for its removal, then write a prompt for how to do so in a separate worktree.

1. Comb over this prompt. Identify any areas that don't make sense and clarify them. ✅ (done in `clarifications.md`)
   1.5. Take a pass at adding the skills and make sure we agree on each one.
2. Create `spike.md` and flesh out all design documents. Name tables, add columns, align on what configuration is necessary. Agree on desired mermaid diagrams (mix of state machines and sequence diagrams).
3. Dispatch a sub-agent to plan each milestone sequentially. Direct it to make changes only inside its specified domain. If it finds the system doesn't support the changes it's supposed to make, command it to come back with context rather than make a product decision itself. When the agent is done, carefully evaluate whether our plan was wrong or incomplete. Treat each milestone as a chance to invalidate our design decisions. Instruct the agent to make isolated commits — especially schema changes, prefixed with `mig:`. We'll make a clearly-denoted `[milestone]` commit at each stage.
4. Go back through the milestones and document tickets to include in each. Clearly document dependency ordering.
5. Sync `spike.md` to Notion as an RFC. Open a stack of draft pull requests, one per milestone in isolation. Synchronize `project.md` to Linear.
