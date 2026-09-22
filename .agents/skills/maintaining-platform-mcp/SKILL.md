---
name: maintaining-platform-mcp
description: Use when adding or changing backend APIs, dashboard workflows, permissions, user-facing product features, or files under server/internal/platformmcp, and when reviewing whether a feature needs a new or updated Platform MCP tool.
---

# Maintaining Platform MCP

The Platform MCP is a first-party product surface, not a separate integration to update later. When a change adds or alters a useful AI Control Plane outcome, assess the Platform MCP in the same change and keep its tools aligned where appropriate. This skill is the detailed source of truth; `CLAUDE.md`, `REVIEW.md`, and `cubic.yaml` contain concise activation and review rules that should stay aligned with it.

## When a product change belongs in Platform MCP

Add or update a tool when the changed capability is a useful, repeatable outcome for an authenticated administrator or member to inspect or manage through an agent. Strong candidates include dashboard workflows, management operations, diagnostics, setup and repair actions, access controls, and product state users reasonably expect the AI Control Plane to understand.

Do not create a tool merely because a new API endpoint exists. Avoid exposing internal implementation operations, unbounded low-level primitives, secrets, workflows that cannot preserve the dashboard's safety controls, or capabilities whose authorization and product contract are not ready. Prefer one outcome-oriented tool over a mechanical copy of each HTTP endpoint.

For every relevant product change, make and document one of these decisions in the implementation or review:

- update an existing Platform MCP tool because its schema, behavior, terminology, permissions, or result is now stale;
- add a new Platform MCP tool because no existing tool represents the user outcome;
- make no Platform MCP change because the capability is internal, unsafe, redundant, or not useful through an agent.

Record the outcome, target resource, actor, existing-tool comparison, update/add/omit decision with rationale, and evidence that proves success. A silent omission is not a decision.

## Source map

| Concern                                                              | Source                                                                       |
| -------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| Tool manifests and handlers                                          | `server/internal/platformmcp/tool_*.go`                                      |
| Deployment-wide registration and server instructions                 | `server/internal/platformmcp/tools.go`                                       |
| Audience, authorization, discovery, schema, and invocation contracts | `server/internal/platformmcp/descriptor.go`                                  |
| Services and product logic behind tools                              | Other files in `server/internal/platformmcp/` and the owning service package |
| Cross-tool contract tests                                            | `server/internal/platformmcp/descriptor_test.go` and neighboring tests       |
| User-facing multi-tool workflows shipped in the Platform plugin      | `server/internal/plugins/platform_mcp_skills/`                               |
| Contributor guidance for those shipped skills                        | `.agents/skills/authoring-platform-mcp-skills/SKILL.md`                      |

Search in this order before designing a new tool:

1. `tool_*.go` by product nouns and verbs, including synonyms such as pause/disable, share/distribute, and repair/setup;
2. registration and server-wide instructions in `tools.go`;
3. audience, scope, and authorization rules in `descriptor.go`;
4. the dashboard/API implementation and owning service tests, which define the current product safeguards;
5. shipped workflows under `server/internal/plugins/platform_mcp_skills/`.

Extend an existing outcome when that keeps the catalogue clearer. Lifecycle state changes can use `tool_lifecycle_visibility.go` as a concrete neighboring pattern.

## Implementation workflow

1. Describe the changed user outcome, who may perform it, and the evidence that proves success.
2. Inspect related Platform MCP tools and distributed skills. Decide whether to update, add, or intentionally omit a tool.
3. Keep the tool contract outcome-oriented:
   - use clear names, titles, and descriptions in product language;
   - expose bounded typed inputs and outputs, not internal or vendor types;
   - return enough state for an agent to distinguish success, refusal, partial progress, and the next safe action;
   - never expose credentials, tokens, raw sensitive evidence, or hidden-resource signals.
4. Declare `ToolMeta` deliberately:
   - choose external and managed-assistant audiences independently;
   - use `ProjectScopeExplicit` when every caller must name a project, `ProjectScopeDefaultable` only when the product explicitly supports the organization's literal Default project, and `ProjectScopeNone` only when the operation is not scoped to one project;
   - for the external endpoint, preserve connection-time live organization-membership admission through `Runtime.Handler`/`PrepareExternalContext` separately from per-tool `ToolMeta.Authorization`; set live authorization and discovery scopes without relying on tool visibility as enforcement;
   - admit a tool to the managed assistant only when it works under assistant identity and exact-project scoping without an external OAuth connection.
5. For mutations, preserve the product workflow's safeguards. At minimum, check for a fresh target read, exact target selection, explicit confirmation, expected-version or equivalent concurrency protection, a stable idempotency key when replay is possible, atomic audit logging, and a post-mutation live read. Set MCP annotations accurately.
6. Register the tool through the shared `Registrar` path in `tools.go`. When a capability can be disabled or incompletely composed, follow the live/unavailable registration pairs in neighboring `tool_*.go` files: keep the same name, audiences, authorization, project scope, schema, and annotations, then return a bounded readable refusal rather than silently dropping the tool from the catalogue.
7. Update related surfaces when the workflow changes:
   - server instructions in `tools.go` for platform-wide behavior;
   - shipped Platform MCP skills when a multi-tool workflow or named tool changes;
   - setup resources, docs, and tool descriptions when terminology or next actions change.
8. Add focused tests for the live behavior and contract. As applicable, prove connection-time live membership admission in `runtime_test.go`, per-call authorization and discovery filtering, the listed input/output schema, each admitted audience, unavailable registration, secret/hidden-resource omissions, mutation safeguards, and the committed state returned after mutation. Put cross-tool invariants in `descriptor_test.go`; keep tool behavior beside the tool or service tests.

## Review checklist

- [ ] The change explicitly assessed whether an existing or new Platform MCP tool is needed.
- [ ] Existing tools and shipped skills are not stale relative to the changed product behavior.
- [ ] A new tool represents a useful user outcome rather than mirroring an implementation endpoint.
- [ ] Name, description, input/output schema, annotations, refusals, and next actions match the real contract.
- [ ] External and managed-assistant audiences were considered separately.
- [ ] Connection-time live membership, per-tool authorization, project targeting, discovery scopes, and hidden-resource behavior are correct and tested as separate boundaries.
- [ ] Mutations retain confirmation, idempotency/concurrency, audit, and verification safeguards.
- [ ] Disabled dependencies produce a stable readable result where the surrounding catalogue does.
- [ ] Server instructions and distributed Platform MCP skills reference only current tools and workflows.
- [ ] Focused tests prove the relevant contract and privacy boundaries.

## Validation

Run the focused Platform MCP package tests through repository tooling:

```bash
mise run test:server ./internal/platformmcp/
```

This package includes integration tests. If local infrastructure is not running, use `./zero --agent` before treating infrastructure failures as product defects.

When shipped Platform MCP skills change, also follow `.agents/skills/authoring-platform-mcp-skills/SKILL.md`. Then run the normal formatting and server gates appropriate to the complete change.
