# ADR 0003: Unresolved required env-sourced headers fail closed

- Status: Accepted
- Date: 2026-07-08
- Ticket: [AGE-2879](https://linear.app/speakeasy/issue/AGE-2879/allow-attaching-environments-to-mcp-servers)
- Builds on: ADR 0002

## Context

Per ADR 0002 a Remote MCP Header may source its value from the attached
environment (an opt-in header with an empty static value is filled by name-match
at serve time). At request time the matching entry may be absent or empty: wrong
environment attached, no environment attached, environment detached mid-flight
(`environment_id` FK is `ON DELETE SET NULL`), or the entry was never populated.

Remote backends commonly front paid/authenticated upstreams. Silently dropping a
required auth header would proxy an unauthenticated request upstream and surface
a confusing upstream 401 instead of an actionable Gram-side error.

## Decision

- **Required header, unresolved value → fail closed.** Gram returns a clear
  4xx/5xx _before_ proxying (the existing `ConfiguredHeader.Resolve` already
  errors on a required header with an empty value; optionally enrich the message
  to name the missing env var). A required-header-missing request is never sent
  upstream.
- **Non-required header, unresolved value → skip the header** and proxy anyway.
  Optionality means the upstream call is valid without it; there is nothing to
  fail closed on.

## Consequences

- Serve-path resolution distinguishes required vs optional when a reference does
  not resolve.
- Deliberately stricter than external MCP's `BuildHeaders`, which skips all empty
  values regardless of requiredness.
