# Assistant Owner Display — Design

**Date:** 2026-06-26
**Status:** Approved for planning

## Problem

An assistant records who created it, but that attribution is invisible in the
product. Users want to see the **owner** (creator) of an assistant — as a profile
avatar — on both the assistant **card** (in the list/grid) and the assistant
**setup page** (the right-hand overview panel).

Reuse the existing org-home avatar treatment for visual consistency.

## Key finding: no migration needed

The `assistants` table already has a populated `created_by_user_id` column
(nullable `TEXT`, set from the authenticated user at creation time, loaded by
every query into `assistantRecord.CreatedByUserID`). The data exists and is
captured today — it is simply **dropped by the API** in `toHTTPAssistant()`
before the `shared.Assistant` type is returned.

This feature therefore **exposes an existing field**; it does not start tracking
new data and requires no schema change.

## Scope

In scope:

- Expose `createdByUserId` on the `Assistant` API/SDK type.
- Render an owner avatar + name on the assistant card.
- Render an owner row on the assistant setup page RHS panel.

Explicitly out of scope (separate PRs):

- Sparkline consistency between the assistant card and the cost page.
- Backfilling `created_by_user_id` for legacy assistants.

## Three owner states

The display resolves `createdByUserId` against the current org members
(`useMembers()` → `AccessMember[]`) into exactly one of three states:

| State        | Condition                                        | Display                                              |
| ------------ | ------------------------------------------------ | ---------------------------------------------------- |
| **Owner**    | id present AND resolves to a current member      | member avatar (`photoUrl`, initials fallback) + name |
| **No owner** | id absent/empty (never attributed)               | muted placeholder avatar + "No owner"                |
| **Orphaned** | id present but NOT in members (creator left org) | muted placeholder avatar + "Orphaned, no owner"      |

The two non-resolved states are distinct and fall out of the same lookup:
`id == null/empty` → No owner; `members.find(id) === undefined` → Orphaned.

## Backend changes

1. **`server/design/shared/assistants.go`** — add an **optional** `createdByUserId`
   string field to the `Assistant` type (Goa attribute, not required).
2. **`server/internal/assistants/service.go` → `toHTTPAssistant()`** — populate
   the field from `record.CreatedByUserID`. Map empty/NULL to **absent** (leave
   the optional field unset / nil pointer) so the frontend can distinguish
   "never had an owner" from a real id.
3. Regenerate, in order: `mise gen:goa-server` → `mise gen:sqlc-server`
   (no-op — no SQL change) → `mise gen:sdk`. This adds `createdByUserId?: string`
   to the `Assistant` SDK model used by both frontend hooks.

No new query, no migration, no `models.go` change (schema is untouched).

## Frontend changes

### Shared component: `AssistantOwner`

A single presentational component, reused by both surfaces, that:

- Takes `createdByUserId: string | undefined` and a `variant` ("card" | "row").
- Calls `useMembers()` and resolves the three states above.
- Reuses the org-home avatar primitives: `Avatar` / `AvatarImage` /
  `AvatarFallback` from `components/ui/avatar.tsx`, `getInitials` fallback, and a
  name-on-hover `Tooltip`.
- For the non-resolved states, renders a muted placeholder avatar
  (`AvatarFallback`, no image) plus the appropriate label.

Likely location: `components/assistants/assistant-owner.tsx`.

### Card — `pages/assistants/Assistants.tsx` (`AssistantCard`)

Add a "Created by 〔avatar〕 Name" line in the **metadata block** (the
`mb-3 flex flex-col gap-2` group, alongside the model + toolsets rows) — not the
already-busy footer. The member name shows inline and also on avatar hover.

### Setup page — `pages/assistants/onboarding/AssistantDraftPanel.tsx`

Insert a `<Row label="Owner">` in the RHS Overview section **immediately after
the `Model` row** (after line 148), before `Concurrency`. Renders avatar + name
inline via `AssistantOwner` variant="row".

### Data

Both surfaces already have the assistant object (`useAssistantsList` for the
card, `useAssistantsGet` for the setup page); after the SDK regen it carries
`createdByUserId`. `useMembers()` is app-cached (already used by access/observe),
so this typically adds no new round-trip.

## Delivery

- **Single PR**: backend field exposure + SDK regen + frontend. The frontend
  depends on the regenerated SDK type, so splitting adds friction.
- **Changeset** required (`"server": patch` for the API surface change).
- **Title**: `feat:` prefix.

## Testing / verification

- `mise build:server` compiles after the Goa design change + regen.
- `cd client/dashboard && ./node_modules/.bin/tsc -b --noEmit --force` passes
  (catches SDK-type drift the incremental cache hides).
- `pnpm knip` clean (the new `AssistantOwner` export must have a consumer — it
  does, via card + setup page).
- Manual: an assistant created by the current user shows their avatar on card +
  setup page; an assistant with NULL creator shows "No owner"; an assistant whose
  creator is not a current member shows "Orphaned, no owner".

## Open questions

None outstanding. Owner placement (card metadata block, "Created by 〔avatar〕
Name" with name on hover; setup RHS row after Model) and the three-state labels
are confirmed.
