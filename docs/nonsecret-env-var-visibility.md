# Design: viewing non-secret environment variable values after save (AGE-2986)

Issue: https://linear.app/speakeasy/issue/AGE-2986/feat-allow-viewingrevealing-non-secret-env-var-values-after-save
Status: design agreed with Walker (2026-07-13), ready to implement. Deliberately modeled on the existing remote MCP server headers `is_secret` pattern (see "Precedent" below), with two deliberate divergences: redaction format stays as-is, and the non-secret-to-secret flip is allowed without a value (see "Decisions").

## Problem

Once an environment entry is saved, its value is redacted in every API response forever. Fermatcommerce (Pylon #11730) stored a plain base URL and could never read it back. There is no other per-environment mechanism for non-secret config, so "use something else" is not an answer today.

Related but distinct: AGE-1146 (hide/show toggle during entry) is Done, shipped 2026-01-13 in PR #1227 (the Eye/EyeOff toggle at `client/dashboard/src/pages/environments/Environment.tsx:582`). This ticket is about reading a value back after save.

## Precedent: remote MCP server headers

The remote MCP headers feature already solved exactly this problem, and this design copies it wholesale. The pattern:

- Column: `is_secret BOOLEAN NOT NULL DEFAULT FALSE` on `remote_mcp_server_headers` (`server/database/schema.sql:2869`).
- Storage: non-secret values are stored as plaintext; only secret values are encrypted. `encryptValue` skips encryption when `!isSecret` (`server/internal/remotemcp/shared.go:145`). Invariant: `is_secret` ⇔ value column holds ciphertext.
- Read: `revealHeader` (`shared.go:126`) passes non-secret values through verbatim; secret values are decrypted then redacted to a constant `***` for management reads (`redactValue`, `shared.go:158`), or returned cleartext to the proxy (`redacted=false`).
- Update semantics: full replace of mutable fields, with one exception. Omitting the value on a header that is already secret and stays secret preserves the stored value (`preserveStoredValue`, `server/internal/remotemcp/impl.go:626`). Any `is_secret` transition, in either direction, falls outside that exception and therefore requires a value to be supplied (`validateHeaderValueSource`, `impl.go:787`). This is the rule that stops write access from becoming secret read access: you cannot flip a secret to non-secret and read the old value back.
- Goa surface: optional `is_secret Boolean` on create/update forms, documented "Defaults to false" (`server/design/remotemcp/design.go:370,386`); required `is_secret` on the view type (`design.go:484`).
- Audit: standard create/update/delete events with before/after redacted views, no flip-specific event.
- Dashboard: no UI consumes the header endpoints yet (SDK hooks exist unused), so UI parity is not copyable; the env UI below is new work either way.

## Current state of environments

Storage and crypto:

- `environment_entries` table has only `name` and `value` (`server/database/schema.sql:672`). No secrecy flag.
- All values are encrypted with AES-256-GCM before insert (`server/internal/environments/shared.go:239`, `server/internal/encryption/encryption.go:86`).

Read path:

- `ListEnvironments` always calls `ListEnvironmentEntries(ctx, projectID, envID, true)` with a blanket `redacted=true` (`server/internal/environments/impl.go:175`).
- `redactedEnvironment` (`server/internal/environments/shared.go:296`) returns the first 3 characters plus `*****`, or `<EMPTY>`. Note this already leaks a 3-char prefix of every value; the headers feature redacts to a constant `***` instead.
- Goa: `EnvironmentEntry.value` documented as "Redacted values" (`server/design/shared/tools.go:258`); `EnvironmentEntryInput` requires `name` and `value` (`server/design/environments/design.go:293`).

Runtime consumption decrypts fully and is unaffected: `Load`, `LoadSourceEnv`, `LoadToolsetEnv`, `LoadMCPAttachedEnvironment`, `LoadSystemEnv` (`shared.go:36-176`), gateway proxy (`server/internal/gateway/proxy.go:1063`).

Authorization and audit: mutations require `authz.ScopeEnvironmentWrite`, listing requires `ScopeProjectRead` (`impl.go:84,181`); `LogEnvironmentCreate/Update/Delete` audit events exist (`impl.go:155,310,500`).

Dashboard: saved values render as the redacted string from the API; entry inputs are `type="password"` with an Eye toggle (`client/dashboard/src/pages/environments/Environment.tsx:520-587`). Same masked treatment in `EnvironmentVariableRow.tsx`, `AddVariableSheet.tsx`, and `PlaygroundAuth.tsx` (`PASSWORD_MASK`).

## Recommended design: port the header pattern to environment entries

### Data model

Migration (follow the `postgresql` skill):

```sql
ALTER TABLE environment_entries ADD COLUMN is_secret boolean NOT NULL DEFAULT true;
```

The DB default stays `TRUE` permanently, unlike the header column's `FALSE`. Existing rows were entered under a promise of secrecy and must backfill to `true`, and Atlas generates migrations declaratively from `schema.sql`, so a backfill-one-way-default-another-way two-step is not expressible (hand-editing migrations is forbidden). The mismatch is harmless: the application always sets the flag explicitly on insert, so the DB default only ever applies to the backfill. The API-level default remains `false` per header convention. Generated as `server/migrations/20260713194550_environment-entries-add-is-secret.sql`; ships in its own PR per migration rules.

Storage matches headers: non-secret values stored plaintext, secret values encrypted, `is_secret` ⇔ ciphertext. `CreateEnvironmentEntries` and `UpdateEnvironmentEntry` (`shared.go:239,272`) gain the same conditional-encryption branch as `encryptValue`; `ListEnvironmentEntries` and the runtime `Load*` functions gain the matching conditional-decryption branch. Clone queries copy `is_secret` and the value column verbatim in both modes (works unchanged since no app-layer decryption happens during clone).

Alternative considered: keep encrypting everything and use `is_secret` only for redaction. Slightly simpler code (no conditional branches), but breaks the header invariant and diverges from the pattern we are matching. Rejected for consistency.

### Update semantics and the flip rule (copied from headers)

- `EnvironmentEntryInput`: `value` becomes optional; add optional `is_secret Boolean` documented "Defaults to false. Omit value on an existing secret entry to preserve its stored value."
- Preserve rule: omitted/empty value on an entry that exists, is secret, and stays secret preserves the stored value (the `preserveStoredValue` logic from `remotemcp/impl.go:626`).
- Flip rule, secret to non-secret: requires a new value in the same request; reject with an invalid-argument `oops` error otherwise. Without this, `ScopeEnvironmentWrite` silently becomes secret read access. This is the one hard constraint.
- Flip rule, non-secret to secret: allowed without a value. The stored plaintext is encrypted in place and the entry becomes secret from then on. This diverges from the headers' symmetric require-a-value rule on purpose (decided 2026-07-13): the direction is safe, and forcing users to re-enter a value they could already see fabricates a problem. Omitted value on a non-secret-to-secret flip means "keep the current value, encrypt it"; a supplied value wins as usual.

### Read path

- `EnvironmentEntry` view type: add required `is_secret Boolean`; `value` is cleartext when `is_secret` is false, redacted otherwise.
- Replace the blanket `redacted bool` in `ListEnvironmentEntries` with per-entry logic keyed on `is_secret`, mirroring `revealHeaders`.
- Redaction format: unchanged (decided 2026-07-13). Secret values keep today's `redactedEnvironment` output (3-char prefix + `*****`, `<EMPTY>` for empty). No switch to the headers' `***`; existing users rely on the prefix to identify keys, and changing it buys nothing this ticket needs.

### API surface

No new endpoints; `is_secret` rides the existing create/update/list methods. Regenerate Goa gen, OpenAPI, and the TS SDK per the `gram-management-api` skill. Audit stays on the existing `LogEnvironmentUpdate` events, matching the headers' approach of standard events with before/after redacted views.

### Dashboard

- Environment detail page (`Environment.tsx`): "Secret" checkbox on the new-entry form, default on. Non-secret rows render cleartext in a normal text input with a copy button and no Eye toggle; secret rows unchanged. A per-row lock button flips secrecy: locking a readable entry needs no value; unlocking a secret entry forces "enter a new value" mode with helper text, mirroring the server rule, and the save handler pre-validates it.
- Deferred to a follow-up (decided during implementation): the MCP add-variable sheet toggle, the MCP `EnvironmentVariableRow` cleartext display, and `PlaygroundAuth` cleartext display. The sheet toggle is useless until the MCP row can display non-secret values, and plumbing the flag through that page's state machine is its own change. Flagless writes from those surfaces keep creating secret entries, so nothing regresses in the meantime.

### Rough sequencing

1. Migration + sqlc regen (deployable alone; nothing reads the column yet).
2. Goa design + gen + server impl + tests: conditional encryption/decryption, per-entry redaction, preserve rule, flip rule, clone.
3. SDK regen.
4. Dashboard: environment page, then MCP sheet.
5. Follow-ups: MCP row / Playground cleartext display; reply on Pylon #11730.

### Skills to activate when implementing

`postgresql` (migration), `golang`, `gram-management-api` (design + gen + SDK), `frontend`, `gram-audit-logging` (only if enriching events). Feature flag likely unnecessary: existing rows backfill secret, so nothing changes until a user opts an entry out.

## Decisions (Walker, 2026-07-13)

1. Dashboard toggle for new env entries defaults to secret (on), since env values are usually credentials. ~~The API default stays `false` per header convention.~~ **Amended during implementation (pending Walker's confirmation):** an omitted `is_secret` means secret for new entries and keep-current-flag for existing entries, NOT false. The header convention is unsafe here: environments predate the flag, so callers that never send it (older dashboards, CLI binaries, external SDK automation) would silently start writing credentials as readable plaintext. With the amended default, every pre-existing caller behaves byte-for-byte as before; only callers that explicitly send `is_secret: false` opt out. The dashboard always sends the flag explicitly either way.
2. Redaction format for secrets stays exactly as today (`abc*****` prefix style). No change.
3. Flip rules take the most flexible safe shape: secret to non-secret requires a new value (the one real constraint); non-secret to secret is allowed value-free, encrypting the stored plaintext in place.
4. AGE-1146 was already Done (shipped 2026-01-13, PR #1227). Nothing to do.

## Open questions

1. Audited reveal of true secrets (a separate `revealEntry`-style endpoint): file as its own ticket or explicitly decide Gram never reveals secrets? The headers feature never reveals either, so "never" is the consistent answer.
