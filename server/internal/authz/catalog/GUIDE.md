# Option B, case by case

This guide answers one question at a time: "how would option B handle X?" Each
section shows the code that answers it and then says, in plain words, what that
code does when a request arrives. New questions are added at the end.

Option B in one paragraph: a **catalogue** of permission definitions
(`permissions.go`) sits on top of the grant storage and engine that Speakeasy
already has (`server/internal/authz`). A service describes the resource it
loaded as a `Target`, asks for a `Request` through a small builder, and a
`Client` checks the request against its definition before the existing engine
reads any grant. The flow is in `doc.go`.

Each section is labelled:

- **Implemented**: the code exists on this branch and has tests.
- **Sketched**: proposed code that is not wired in or not tested. It is
  written to show the shape and may not compile as is.

## Questions

1. [How would the definition of project authz look?](#1-how-would-the-definition-of-project-authz-look)
2. [How would checking authz when connecting to an MCP server look?](#2-how-would-checking-authz-when-connecting-to-an-mcp-server-look)

---

## 1. How would the definition of project authz look?

**Status:** the definitions and builders are implemented and tested
(`permissions.go`, `project.go`, `project_test.go`). No service uses them yet;
the call-site change below is sketched.

### Short answer

A project permission is two entries in the same registry as the MCP ones. They
encode today's project rules, unchanged: `project:write` lets a caller view the
project too, a grant may name one project or the whole organization, and a
direct grant to a user outranks a block that user inherits from a role.

### The definition

`permissions.go:76`

```go
{
	Name:              authz.ScopeProjectRead,
	Resource:          authz.ResourceKindProject,
	AllowedAnchors:    []string{authz.ResourceKindProject, authz.ResourceKindOrg},
	RequiredAncestors: projectAncestors, // {org}
	ImpliedBy:         []authz.Scope{authz.ScopeProjectWrite},
	ExcludedBy:        authz.ScopeProjectBlockedRead,
	BlockWins:         false,
},
{
	Name:              authz.ScopeProjectWrite,
	Resource:          authz.ResourceKindProject,
	AllowedAnchors:    []string{authz.ResourceKindProject, authz.ResourceKindOrg},
	RequiredAncestors: projectAncestors, // {org}
	ImpliedBy:         nil,
	ExcludedBy:        authz.ScopeProjectBlockedWrite,
	BlockWins:         false,
},
```

Read field by field, `project:write` means:

| Field | Value | In plain words |
| --- | --- | --- |
| `Resource` | `project` | The thing being checked is one project. A check on "every project" is a programming error, not a deny. |
| `AllowedAnchors` | project, org | A grant can name one project or cover the whole organization. Both are normal, so neither is flagged as legacy. |
| `RequiredAncestors` | org | The caller must say which organization the project belongs to. If it is not the caller's active organization, the answer is "cross-tenant" before any grant is read. |
| `ImpliedBy` | nothing | Only a `project:write` grant (or `root`) allows it. `project:read` is implied by `project:write`, one step and written down. |
| `ExcludedBy` | `project:blocked_write` | A matching block withdraws it. |
| `BlockWins` | false | Today's precedence: a block from a role or from "all users" loses to a grant made directly to that user for that project. A block and an allow from the same role: the block wins. |

The MCP definitions set `BlockWins: true` instead, because slice 1 removed that
override for MCP servers. The two families sit side by side in one table, so
the difference is visible in the code rather than hidden in a precedence list.

### How a service asks

`project.go`

```go
target := catalog.ProjectTarget(authCtx.ActiveOrganizationID, authCtx.ProjectID.String())
decision, err := client.Require(ctx, catalog.ProjectWrite(target))
```

`ProjectTarget` builds the project plus its organization. The project comes
from the caller's verified session or a loaded project row, never from a field
in the request body.

**Sketched:** today about 200 call sites build the check by hand, for example
`packages/impl.go:133`:

```go
// today
s.authz.Require(ctx, authz.Check{Scope: authz.ScopeProjectWrite, ResourceKind: "", ResourceID: authCtx.ProjectID.String(), Dimensions: nil})

// with the catalogue (not done on this branch)
_, err := s.authzClient.Require(ctx, catalog.ProjectWrite(catalog.ProjectTarget(authCtx.ActiveOrganizationID, authCtx.ProjectID.String())))
```

Both reach the same engine decision. The catalogue version adds the
organization check and the "one concrete project" check before the grants are
read, and returns a `Decision` that says why.

### How it behaves

These rows are `TestProject_definitionKeepsTodaysRules` in `project_test.go`.
Every row is checked through both `Authorize` (used for "can I?" answers in
the UI) and `Require` (used to enforce), and both must agree.

| Caller holds | Asks for | Answer |
| --- | --- | --- |
| `project:write` on P1 | write P1 | allowed |
| `project:write` on P1 | read P1 | allowed (write implies read) |
| `project:write` on P1 | write P2 | denied: no grant |
| `project:read` on every project, via a role | read P2 | allowed |
| `project:read` on every project, via a role | write P1 | denied: no grant |
| direct `project:write` on P1, plus a role's `project:blocked_write` on P1 | write P1 | allowed: the direct grant outranks the inherited block |
| a role's `project:write` and `project:blocked_write` on P1 | write P1 | denied: blocked |
| `project:write` on P1 | write P1 in another organization | denied: cross-tenant, before grants are read |

### What this does not change

- The engine keeps its own tables (`authz/scopes.go`, `authz/precedence.go`),
  because `authz` cannot import the catalogue. `TestDefinitionsMatchEngine`
  fails if the project entries ever disagree with them.
- Nothing about storage, roles or the dashboard changes. Adding the
  definitions alone changes no behaviour, because no service calls them yet.
- Organization-level permissions (`org:read`, `org:admin`) are not defined
  yet. They would follow the same pattern with no required ancestor.

---

## 2. How would checking authz when connecting to an MCP server look?

**Status:** implemented in slice 1 for the private remote-backed endpoint
proxy and its consent paths. Walkthrough 01
(`walkthroughs/01-mcp-server-configure-vs-use.md`) follows one request in
detail with diagrams; this section is the short version.

### Short answer

Connecting asks for `mcp:connect` on that one server, inside its project and
organization. Nothing else satisfies it: being able to configure the server
(`mcp:write`) or view it (`mcp:read`) does not. A matching block always wins,
including over a grant made directly to the user. A public server needs no
server-level permission.

### The definition

`permissions.go:67`

```go
{
	Name:              authz.ScopeMCPConnect,
	Resource:          authz.ResourceKindMCP,
	AllowedAnchors:    []string{authz.ResourceKindMCP},  // one server only
	RequiredAncestors: mcpServerAncestors,               // {project, org}
	ImpliedBy:         nil,                              // management never implies use
	ExcludedBy:        authz.ScopeMCPBlockedConnect,
	BlockWins:         true,
},
```

### What happens on a request

1. **Identity first, in the service.** The endpoint handler authenticates the
   caller, refuses a caller from another organization, and loads the server row
   and its project (`mcp/serveendpoint.go:915` onwards). None of this is
   authorization policy.
2. **The service describes what it loaded** (`mcp/serveendpoint.go:1056`):
   ```go
   target := catalog.MCPServerTarget(organizationID, projectID.String(), mcpServer.ID.String())
   ```
   The organization and project come from the endpoint's own records, not from
   the request.
3. **The builder says what using it requires** (`mcpserver.go:55`):
   ```go
   requests, err := catalog.MCPServerUse(target, catalog.MCPServerVisibility(mcpServer.Visibility))
   ```
   Public: no requests, so the call returns allowed. Private: one `mcp:connect`
   request. Any other visibility is an error, so it fails closed.
4. **The engine loads the caller's grants** (`s.authz.PrepareContext`,
   `mcp/serveendpoint.go:1084`), exactly as before.
5. **The client decides** (`mcp/serveendpoint.go:1092`, `client.go:137`):
   ```go
   decision, err := catalog.NewEngineClient(s.authz).Require(ctx, requests...)
   ```
   - `prepare` (`client.go:162`) checks the definition: one concrete server;
     the server's organization is the caller's; the project and organization
     are both present. A failure here is denied ("cross_tenant") or unresolved
     ("missing_fact") without reading grants.
   - The backend (`backend.go`) asks the existing engine. The engine looks for
     an `mcp:connect` grant on that server whose project, if it names one,
     matches; then applies `mcp:blocked_connect`, which always wins.
   - `decide` (`client.go:235`) returns allowed, denied ("no_grant" or
     "blocked") or unresolved. If the allow came from an old wildcard connect
     grant, it is labelled `legacy_broad_grant` and logged.
6. **On a deny**, the service returns 403 with a request-access link
   (`mcp/serveendpoint.go:1098`). The upstream server is never contacted.

### How it behaves

From `TestClient_authorizeAgreesWithRequire` (`client_test.go`) and the
end-to-end test `mcp/configure_without_use_test.go`:

| Caller holds | Connect to S1 | Connect to S2 |
| --- | --- | --- |
| `mcp:write` on S1 (can configure it) | **denied** | denied |
| `mcp:connect` on S1 | allowed | denied |
| direct `mcp:connect` on S1, plus a role's `mcp:blocked_connect` on S1 | **denied: blocked** | denied |
| `mcp:connect` on S1 scoped to its project | allowed | denied |
| `mcp:connect` on every server (old wildcard grant) | allowed, logged as legacy | allowed, logged as legacy |
| grants on a server in another organization | denied: cross-tenant | - |

### What this does not cover

- **Tool calls on an open connection.** The per-tool interceptors and about 18
  other production call sites still build `authz.MCPCheck(ScopeMCPConnect, ...)`
  by hand instead of going through the client. They get the same answer,
  because slice 1 changed the engine's own tables (nothing implies connect;
  MCP blocks always win), but they skip the client's organization and project
  checks.
- **Lists and explanations.** The SQL that lists accessible servers, the
  dashboard's `useRBAC`, the access-list API and the explain endpoint still
  apply the old rules, so they can disagree with the check until each is moved.
- **Stored roles.** A custom role that holds only `mcp:write` loses the ability
  to connect. There is no migration for it yet.
