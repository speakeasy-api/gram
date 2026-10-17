# Production-shaped handler examples

These examples show how the authorization client would fit into existing
Speakeasy domains. They compile against the local authorization package but do
not replace production handlers. Authentication, repository, and feature-check
interfaces describe integration obligations; there are no permissive default
implementations in the handlers. Dedicated scenario executables supply explicitly
named fixtures. The existing `../main.go` remains the separate broad showcase.

We add and review one use case at a time. Each domain gets its own directory;
each handler includes only the scaffolding needed to explain its access boundary.

Startup constructs a shared client using `authz.AllDefinitions()`, the central
catalog of every permission family currently modeled. The catalog assigns no
grants or defaults. Fixtures may replace policy between scenarios; handlers do not
construct clients or register their own domain's definitions.

## Projects: list visibility and detail agreement

[`projects/list.go`](projects/list.go) sketches the access portions of
`server/internal/projects/impl.go`, `ListProjects` and `GetProject`. The production
list's existing session and organization-membership admission remains required;
this example starts after that boundary. The subject is passed explicitly because
the local client does not yet read a trusted subject from context. No additional
identity-provider abstraction or reference-only query is introduced.

The business rule is that a project list exposes only rows permitted by
`project:read`, and an equivalent detail read agrees when policy and target facts
have not changed. The handlers reuse ordinary tenant-scoped project rows:

- `List` adapts rows to targets, calls `Filter` once, and projects only allowed
  rows into the response, preserving repository order.
- `Get` loads the existing row, calls `Require` for the same permission and target,
  and returns no project metadata if authorization refuses.
- `Project.AuthzTarget()` supplies the project and its canonical organization
  ancestor. It neither grants access nor rewrites repository scoping.

[`projects/visibility/main.go`](projects/visibility/main.go) runs those actual
handlers with one shared client and a tenant-scoped fixture repository:

```sh
mise exec -- go run ./examples/authz-spike/usecases/projects/visibility
```

It verifies these outcomes, and detail reads for A, B, and C must agree with each
list result. Hidden detail reads must fail specifically with `authz.ErrDenied` and
return no project row, rather than fail because the fixture omitted the resource.

| Direct user policy                                     | Visible projects                                           |
| ------------------------------------------------------ | ---------------------------------------------------------- |
| Read A, write B, and block read B; no grant for C      | A only                                                     |
| Organization-wide project read, with read blocked on B | C then A, preserving repository order                      |
| Write B, with only write blocked on B                  | B; read remains available                                  |
| Organization admin only                                | None; organization admin is not a project-read implication |
| Remove all direct grants after the earlier scenarios   | None; later detail reads also refuse                       |

A row in another organization reuses A's ID. The repository must exclude it, and
the returned rows/details are checked against the intended tenant's data. Grants
are fixture assignments, not authorization to edit policy. This does not verify
real session authentication, membership, directory resolution, or inherited-block
exceptions.

**Collection boundary:** this uses the current bounded `Filter`, capped at 100
candidates. It is not a database lookup/pagination design. The example has no
unfiltered total, page cursor, or facets, and must not be adapted by filtering one
arbitrary database page and claiming a complete visible collection. Separate
requests can legitimately differ after policy/resource changes; snapshot coherence
does not span the list request and every subsequent detail request. Concurrent
moves, distributed freshness, safe error mapping, and production response assembly
remain integration obligations. No production handler or query is changed.

## MCPs: connection admission is not tool authorization

[`mcps/invoke.go`](mcps/invoke.go) shows private connection admission and the
per-tool gate represented by `server/internal/mcp/rpc_tools_call.go`. The handlers
reuse the shared client and ordinary server lookup. A request supplies a server ID
and tool name, never trusted ancestry or a disposition classification.

`Connect` uses `DiscoverAccess` to prove some effective access remains, then opens
the upstream connection only for an explicit allow. This is policy-region
discovery, not proof that a permitted tool exists in the current server catalog.
`Invoke` loads the actual tool's metadata and calls `Require` with tool facts before
upstream execution. It does not reuse the connection's earlier allow. Unknown facts
can produce an unresolved decision, which `Require` refuses just like a denial.

Run the handlers with a capture runtime:

```sh
mise exec -- go run ./examples/authz-spike/usecases/mcps/toolaccess
```

The executable verifies these business cases:

| Direct user policy                                             | Connection                         | Concrete invocation                                                              |
| -------------------------------------------------------------- | ---------------------------------- | -------------------------------------------------------------------------------- |
| Project MCP connect restricted to read-only tools              | Admitted                           | Read-only `list` executes; destructive `delete` and unclassified `lookup` refuse |
| Project MCP connect, with only `delete` blocked on this server | Admitted                           | `list` and `lookup` execute; `delete` refuses                                    |
| Unrestricted project MCP connect                               | Admitted                           | All three tools execute, including the unclassified tool                         |
| Project MCP connect plus a server-wide connect block           | Refused before upstream connection | No upstream work is started                                                      |
| Project read only                                              | Refused before upstream connection | Project containment does not imply MCP authority                                 |

Each refused invocation must wrap `authz.ErrDenied`, return no result, and leave
the upstream-call count unchanged. For every admitted scenario, removing all
direct grants while retaining the same connection makes the next invocation fail
without an upstream call. This verifies live memory-policy checks, not a production
cache-invalidation deadline or cancellation of a call already in progress.

**Coverage boundary:** this models a private, directly authenticated user path with
synthetic metadata and transport. Real MCP session-to-subject/resource binding,
OAuth/upstream credentials, public endpoints, risk approval, billing, tool-list
projection, and classification accuracy/reclassification are not implemented.
The single disposition bucket is also not a complete representation of overlapping
production annotations. Connect/read/write block independence and agent/delegator
intersection need their own cases. Inherited-block/direct-user exception precedence
remains unimplemented; this example assigns all grants directly to the user. No
production handler or protocol implementation is changed.

## Skills: read one skill

[`skills/get.go`](skills/get.go) sketches the access boundary in
`server/internal/skills/impl.go`, `Get`, `requireSkillAccess`, and
`requireSkillGrant`. Generated request/response types, response assembly,
statistics, and HTTP error mapping are deliberately omitted.

The handler reads a trusted subject/project scope, resolves the skill's actual
organization/project ancestry, requires `skill:read`, checks feature availability,
and only then loads protected content. `SkillReference.AuthzTarget()` owns resource
adaptation. The handler neither loads roles nor interprets grant precedence.

The first business rule is that resource reads require the corresponding effective
authority. A direct skill-read grant, an enclosing project/organization skill-read
grant, or a declared skill-write implication can satisfy the check. Project read
alone and an empty effective policy cannot. An authorization error or unresolved
decision prevents content loading. Equivalent feature availability is still required.

This is a proposed target-based replacement for the current project/skill
alternative checks, not proof of production equivalence. The authorization package
does not yet implement production's direct-user exception to inherited blocks.
Blocking behavior is exercised below; delegation will be reviewed separately.
Real middleware must authenticate the subject, and the
repository must enforce its tenant/project lookup and resource-move contracts.

Compiling the handler checks interface compatibility, not these runtime guarantees.
The scenario below exercises it with fixture implementations rather than a real
database or authentication provider. No test files are added.

## Skills: blocking grants and protected content

[`skills/blocking/main.go`](skills/blocking/main.go) runs the **same `Service.Get`
handler**, rather than implementing a second authorization path. It verifies that
a matching skill-read block defeats broader skill access, and that a refusal stops
the handler before content loading. The fixture repository counts protected reads;
an expected refusal must wrap `authz.ErrDenied`, not merely fail during setup.

Run this example independently:

```sh
mise exec -- go run ./examples/authz-spike/usecases/skills/blocking
```

The executable checks these outcomes and exits unsuccessfully on any mismatch:

| Policy assigned directly to the user                        | Read request    | Required outcome                                            |
| ----------------------------------------------------------- | --------------- | ----------------------------------------------------------- |
| Project-wide skill write                                    | Skill A         | Allowed through write-to-read implication; one content read |
| Project-wide skill write plus skill-read block on A         | Skill A         | Denied; zero content reads                                  |
| Direct skill read plus the user's own skill-read block on A | Skill A         | Denied; zero content reads                                  |
| Project-wide skill write plus skill-read block on A         | Sibling Skill B | Allowed; A's block does not apply                           |
| Project-wide skill write plus skill-write block on A        | Skill A         | Read remains allowed; write-only block does not remove read |

The handler is unchanged: it still asks for `SkillRead` on the trusted target.
It does not special-case broad grants, blocks, or assignment order. Skill permission
definitions and the evaluator own those decisions.

**Coverage boundary:** all grants in these cases are assigned directly to the
user. This verifies the user's own block, not the complete production precedence
contract. A concrete direct-user allow overriding an inherited role/`user:all`
block remains unimplemented and must not be counted as verified. Agent grants,
write-handler refusal, list filtering, delegation, real transport authentication,
and database concurrency are also outside this example. The fixtures establish
local handler behavior, not production security equivalence.
