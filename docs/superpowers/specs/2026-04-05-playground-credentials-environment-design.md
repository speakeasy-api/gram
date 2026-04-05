# Playground Credentials via Server-Side Environments

## Problem

User-provided playground credentials (API keys, tokens) were stored in `localStorage`, which is vulnerable to XSS and not cleaned up on logout. Credentials should be stored server-side using the existing encrypted environment system.

## Design

### Approach

Create a per-user per-toolset environment lazily when the user first saves credentials in the playground auth tab. Credentials are stored encrypted in the database via the existing environment entries system. The server resolves real values when proxying to the MCP server — raw credentials never reach the browser after initial entry.

### Environment Naming

- **Slug**: `playground-<userID>-<toolsetSlug>`
- **Name**: `"Playground - <displayName>"`
- Created lazily on first credential save
- Not linked via `setToolsetEnvironmentLink` (avoids conflicting with team-level default)

### Data Flow

**Writing (on credential input):**

1. User types a credential value in the playground auth input
2. Debounced (~1s) call to `createEnvironment` (first time) or `updateEnvironment` (subsequent)
3. Values stored encrypted on the server via existing environment entry system

**Reading (on mount):**

1. Look up environment by slug from `useListEnvironments()` data
2. If found, entries exist but values are redacted (server always returns masked values)
3. Show `"••••••••"` mask for entries that have stored values
4. Empty fields shown for entries with no stored value

**MCP Proxy Resolution:**

- Pass the playground environment slug via `Gram-Environment` header
- Server decrypts and injects entries when proxying to the MCP server
- Raw credential values never leave the server after initial storage

### Frontend Changes

**Revert localStorage approach:**

- Remove `useLocalStorageState` from `PlaygroundAuth.tsx`, revert to `useState` for ephemeral input state
- Remove `userProvidedHeaders` state from `Playground.tsx`
- Remove `onUserProvidedHeadersChange` callback chain

**New `usePlaygroundEnvironment` hook:**

- Derives slug: `playground-${user.id}-${toolset.slug}`
- On mount: finds environment from `useListEnvironments()`, checks which entries exist
- Pre-populates fields with `PASSWORD_MASK` for stored entries
- Exposes `save(entries)` function with debounced create/update
- Handles create-on-first-save vs update-on-subsequent logic

**`PlaygroundAuth.tsx`:**

- Use `usePlaygroundEnvironment` hook for persistence
- Masked fields: when focused and edited, treat as new value
- Untouched masked fields: don't include in update (already stored)

**`PlaygroundElements.tsx`:**

- Remove `userProvidedHeaders` from API headers
- Use playground environment slug as `gramEnvironment` when it exists
- Fall back to toolset's default environment otherwise

**`Playground.tsx`:**

- Remove `userProvidedHeaders` state and `onUserProvidedHeadersChange` prop threading

### Edge Cases

- **Environment doesn't exist yet**: show empty inputs, create on first save
- **User clears a value**: include in `entries_to_remove` on next update
- **Toolset slug changes**: old environment orphaned (acceptable, no sensitive data leak since it's encrypted and project-scoped)
- **Multiple browser tabs**: `useListEnvironments` refetches on window focus, so tabs stay reasonably in sync

### Files to Modify

| File                                                                | Change                                                         |
| ------------------------------------------------------------------- | -------------------------------------------------------------- |
| `client/dashboard/src/pages/playground/PlaygroundAuth.tsx`          | Revert localStorage, integrate `usePlaygroundEnvironment` hook |
| `client/dashboard/src/pages/playground/PlaygroundElements.tsx`      | Use playground env slug instead of `userProvidedHeaders`       |
| `client/dashboard/src/pages/playground/Playground.tsx`              | Remove `userProvidedHeaders` state                             |
| `client/dashboard/src/pages/playground/usePlaygroundEnvironment.ts` | New hook (create)                                              |

### No Backend Changes Required

All functionality uses existing environment CRUD APIs:

- `createEnvironment` — create environment with initial entries
- `updateEnvironment` — upsert/remove entries
- `listEnvironments` — find by slug on mount
- `Gram-Environment` header — server-side credential resolution
