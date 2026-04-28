# OAuth Wizard — FSM Redesign

Status: **Phase 3 in progress — edit lifted out of the FSM**
Author: walker
Date: 2026-04-28

## Progress

- [x] **Phase 1 — scaffold the machine alongside the reducer.** Commit
      `2f464abd1`. Adds `machine.ts`, `machine-types.ts`, `guards.ts`,
      `services.ts`, `machine.test.ts`. xstate 5.30 + @xstate/react 6.1.
      Production code unchanged; 50 unit tests pass.
- [x] **Phase 2 — container swap + cleanup.** Commit `d18d7e71f`.
      `OAuthWizard.tsx` runs on `useMachine`; step components read
      from xstate state and call `send`; `FatalErrorStep` added;
      `reducer.ts` / `actions.ts` / `types.ts` deleted; RTL tests added.
- [ ] **Phase 3 — lift edit out of the FSM.** Move the edit flow to a
      standalone `EditOAuthProxyModal` (plain form +
      `useUpdateOAuthProxyServerMutation`); strip `mode`,
      `editProxyDefaults`, `audiencePrefilled`, `audienceDirty`,
      `isEditMode`, `SUBMIT_EDIT`, `proxy.updating`, and the
      `updateOAuthProxy` actor from the FSM. Edit no longer goes
      through the wizard machine.

## 1. Background

The OAuth wizard guides a user through wiring an MCP server's OAuth either via
an "External OAuth" handoff or via Gram's "OAuth Proxy". It currently lives in
`client/dashboard/src/pages/mcp/oauth-wizard/` and is built on `useReducer` +
TanStack Query mutations + a handful of refs.

Today's pieces:

| File                                                                                                                | Role                                                         |
| ------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------ |
| `OAuthWizard.tsx`                                                                                                   | Modal container, mutation wiring, conditional step rendering |
| `reducer.ts`                                                                                                        | `WizardState` discriminated union + transitions              |
| `types.ts`                                                                                                          | State, action, and form-data types                           |
| `actions.ts`                                                                                                        | `useStepActions` hook — validation + mutation orchestration  |
| `PathSelection.tsx`, `ExternalOAuthForm.tsx`, `ProxyMetadataForm.tsx`, `ProxyCredentialsForm.tsx`, `ResultStep.tsx` | Per-step views                                               |
| `state-machine-type.ts`                                                                                             | Empty placeholder (signal of prior intent)                   |

## 2. Why this is hard to follow today

Concrete pain points, with file pointers:

1. **Two sources of truth for "did the user change audience?"** — a `useRef`
   in `OAuthWizard.tsx:88-96` shadows reducer state and is then read inside
   `actions.ts:174` to decide whether to send an `audience` field on update.
   This is invisible from the reducer; you can't reason about it from the
   state machine alone.
2. **Nested mutation cascade for the "create proxy" flow** —
   `actions.ts:201-275` calls `createEnvironmentMutation.mutate` and inside
   `onSuccess` calls `addOAuthProxyMutation.mutate`. The intermediate state
   ("env created, proxy not yet") has no name, no UI, and no rollback path.
3. **Implicit loading state** — three separate `mutation.isPending` flags are
   OR'd together in different places (`OAuthWizard.tsx:267,278`,
   `actions.ts:287`). There is no single "the wizard is busy" notion.
4. **`UPDATE_FIELD` is stringly-typed** — `types.ts:72` accepts
   `field: string`, cast as `WizardState`. Refactors silently break.
5. **Validation duplicated three times** — scope-list parsing repeats in
   `actions.ts:127`, `:169`, `:214`.
6. **Edit-mode pre-fill via `useEffect` + dispatch** — `OAuthWizard.tsx:91-112`
   races with the modal's open/close reset effect at `:114-119` (200 ms
   timer). State origin depends on render order.
7. **Step transitions are buried in callbacks, not the reducer** — e.g.
   `proxyCreateSubmit` is what actually moves the wizard to `result`, not the
   reducer; the reducer only handles `SET_RESULT` after the side effect
   resolves. The full flow can't be read top-to-bottom anywhere.
8. **Prop-drilling per step** — every step renderer in
   `OAuthWizard.tsx:238-287` takes a different prop bag (state, dispatch,
   mutations, derived booleans).

The underlying shape **is already a state machine** — discriminated union of
steps, finite events. We're just paying for hand-rolling it.

## 3. Goals / non-goals

**Goals**

- Make the entire wizard flow legible from one file (the machine).
- Move side effects (mutations, env creation) into named, observable states
  so loading/error/retry have first-class places to live.
- Eliminate the ref + state duplication; "is the audience dirty" should be
  derivable from context.
- Make impossible states unrepresentable (e.g. credentials form without
  validated metadata).
- Keep the React tree dumb: views read state + send events, that's it.

**Non-goals**

- Changing UX, copy, or visual design.
- Touching server-side OAuth handling.
- Generalizing into a reusable wizard framework. This machine is for _this_
  wizard.
- Replacing TanStack Query. Mutations stay; we just invoke them from the
  machine.

## 4. Proposed approach: XState v5 + `@xstate/react`

XState v5 fits because:

- States = our existing steps, but hierarchical (proxy/external are siblings
  with their own substates).
- Invoked promises model the mutation cascade explicitly.
- Guards centralize validation.
- `useMachine` from `@xstate/react` gives us a single hook in the container.

Rough sketch (not final API):

```
oauthWizard
├── pathSelection                        (initial)
│
├── external
│   ├── editing                          ← form open
│   │   on:
│   │     SUBMIT  → submitting           (guard: validExternal)
│   │     BACK    → #pathSelection
│   ├── submitting                       (invoke addExternalOAuth)
│   │     onDone  → #result.success
│   │     onError → editing (assign error)
│
├── proxy
│   ├── metadata
│   │   on:
│   │     NEXT    → credentials          (guard: validProxyMeta)
│   │     BACK    → #pathSelection
│   ├── credentials
│   │   on:
│   │     SUBMIT  → creatingEnvironment  (guard: validCreds)
│   │     BACK    → metadata
│   ├── creatingEnvironment              (invoke createEnvironment)
│   │     onDone  → creatingProxy        (assign envSlug)
│   │     onError → credentials (assign error)
│   ├── creatingProxy                    (invoke addOAuthProxy)
│   │     onDone  → #result.success
│   │     onError → rollingBackEnv       (preserve error)
│   ├── rollingBackEnv                   (invoke deleteEnvironment, envSlug)
│   │     onDone  → credentials (assign error from creatingProxy)
│   │     onError → fatalError  (compound error: proxy failed AND env rollback failed)
│   ├── fatalError                       (terminal — orphaned env exists)
│   │     no transitions; user must close modal & clean up env manually
│
└── result
    ├── success
    on:
      RESET → #pathSelection
```

The machine handles **create only**. Editing an existing OAuth proxy is
done by a separate component (`EditOAuthProxyModal.tsx`) — a plain form
backed by `useUpdateOAuthProxyServerMutation`. See §8.6.

### 4.1 Context

A single typed context replaces today's per-step union fields. Steps decide
what to _render_ off `state.matches(...)`, not what fields exist.

```ts
type Context = {
  discovered: DiscoveredOAuth | null;
  external: { slug: string; metadataJson: string; jsonError: string | null };
  proxy: {
    slug: string;
    authorizationEndpoint: string;
    tokenEndpoint: string;
    scopes: string; // raw input; parsed in guard/action
    audience: string;
    tokenAuthMethod: string;
    environmentSlug: string;
    clientId: string;
    clientSecret: string;
  };
  error: string | null;
  result: { success: boolean; message: string } | null;
  toolsetSlug: string;
  toolsetName: string;
  existingEnvNames: string[];
};
```

### 4.2 Events

```ts
type Event =
  | { type: "SELECT_EXTERNAL" }
  | { type: "SELECT_PROXY" }
  | { type: "APPLY_DISCOVERED" }
  | { type: "FIELD"; section: "external" | "proxy"; key: string; value: string }
  | { type: "BACK" }
  | { type: "NEXT" }
  | { type: "SUBMIT" }
  | { type: "RESET" };
```

`FIELD` is typed by section; `key` is constrained to keys of that section
(via `keyof Context["external"] | keyof Context["proxy"]`). This kills the
stringly-typed `UPDATE_FIELD` problem.

### 4.3 Guards

One place each:

- `validExternal` — slug + parseable JSON + required endpoints.
- `validProxyMeta` — slug + endpoints + ≥1 scope.
- `validCreds` — clientId + clientSecret non-empty.

### 4.4 Services / actors

Mutations get wrapped as actors that the machine invokes. These are thin
wrappers around the existing TanStack mutation functions; the queryClient
invalidations stay where they are today, called from the actor's resolved
value via a parent `onDone` action.

- `addExternalOAuthService`
- `createEnvironmentService` → returns `{ envSlug }`
- `addOAuthProxyService`
- `deleteEnvironmentService` → used by `rollingBackEnv`

The `proxy.creatingEnvironment → proxy.creatingProxy` chain becomes two
visible states instead of a nested `onSuccess`. If `creatingProxy` fails,
the machine transitions to `rollingBackEnv`, which deletes the orphaned
environment before returning the user to the credentials form with the
original proxy-creation error. (Per §8.3: we **roll back** on partial
failure rather than resuming with the orphaned env.)

If the rollback **itself** fails, the machine enters `fatalError`. This
is a terminal state — there's no path back to `credentials`, because
retrying from there would call `createEnvironment` again and create a
**second** environment with a suffix, permanently stranding the first.
The view layer in `fatalError` shows the compound error and a "Close"
button; cleanup of the orphaned environment is manual (link to the
Environments page). This is rare (network partition during cleanup, or
the env-delete endpoint regressed) but the wizard must not silently
make it worse.

The rollback path uses the existing `useDeleteEnvironmentMutation` hook
(generated at `client/sdk/src/react-query/deleteEnvironment.ts`, backed
by `/rpc/environments.delete` — hard delete, idempotent, same
`project:write` scope as create). Already in use on the environment
detail page (`client/dashboard/src/pages/environments/Environment.tsx:200-208`),
so wiring it into the machine is mechanical.

### 4.5 Edit-mode entry

Instead of `useEffect` + dispatch, the machine accepts initial input:

```ts
useMachine(oauthWizardMachine, {
  input: { mode, toolset, editProxyServer, discovered },
});
```

`input` populates context once at machine startup.

Edit is **not** modeled in this machine — see §8.6. The wizard is
create-only.

The 200ms reset timer in `OAuthWizard.tsx:114-119` **stays** — it's there
so the modal's close animation completes before content swaps. The shape
changes slightly: instead of `dispatch({ type: "RESET" })` after the
delay, we delay the unmount/remount of the machine so the user doesn't
see content reflow mid-animation.

**Cancellation on close.** When the modal closes mid-flight (e.g. user
hits ESC during `creatingEnvironment`), the machine actor is stopped on
unmount, but the underlying TanStack mutation is not automatically
aborted — `useMutation` doesn't cancel on unmount by default. The
implementation must either (a) signal cancellation via an `AbortSignal`
threaded into each service's `fetch` call, or (b) accept that the
in-flight request resolves into a void; the machine is gone, so its
`onDone`/`onError` won't fire. Option (b) is acceptable for
`addExternalOAuth` (idempotent retries). Option (a) is required for
`creatingEnvironment` — otherwise a successful env-create whose machine
already unmounted leaves an orphan env on the next open. Confirm during
implementation which TanStack mutations already accept `AbortSignal`.

### 4.6 Loading-only states

Several states in §4 have no user input — they exist to invoke an actor
and route on its outcome. They are first-class members of the machine,
not booleans threaded through props:

| State                       | Region   | Service invoked     | onDone                                 | onError                             |
| --------------------------- | -------- | ------------------- | -------------------------------------- | ----------------------------------- |
| `external.submitting`       | external | `addExternalOAuth`  | `#result.success`                      | `external.editing`                  |
| `proxy.creatingEnvironment` | proxy    | `createEnvironment` | `proxy.creatingProxy` (assign envSlug) | `proxy.credentials`                 |
| `proxy.creatingProxy`       | proxy    | `addOAuthProxy`     | `#result.success`                      | `proxy.rollingBackEnv`              |
| `proxy.rollingBackEnv`      | proxy    | `deleteEnvironment` | `proxy.credentials` (assign error)     | `proxy.fatalError` (compound error) |

The view layer reads these via `state.matches(...)`. There is **no**
`isPending` boolean anywhere in the React tree — pendingness is a state.
The container picks the rendering: keep the previous form mounted with a
disabled overlay, or swap in a dedicated loading panel. Both are one
branch in the renderer, no prop drilling.

Adding a new loading-only step in the future (e.g. an auto-prefill
fetch between two existing steps) is the same pattern: one new state
that invokes a service, with `onDone`/`onError` transitions deciding
where to go next. Optional steps gate via a guard on the predecessor's
exit transition — `NEXT → newLoadingState (guard: shouldRun)` vs.
`NEXT → originalNext (guard: !shouldRun)`. No reducer surgery, no new
prop on the form.

## 5. File layout after migration

New: `machine.ts` (machine + context/event types), `guards.ts`,
`services.ts`, `machine.test.ts`. Step components stay; their props
shrink to whatever they read from `state` and `send`. Removed:
`reducer.ts`, `actions.ts`, `types.ts`, `state-machine-type.ts`.

## 6. Migration plan

**One PR.** A standalone machine without the components feeding it events
is dead code to review — reviewers need the UI integration to evaluate
it. So: add the machine, swap the container, update the step components,
and delete the old reducer/actions/types in a single change. The surface
area is small enough (~700 LOC net-neutral) that splitting buys nothing.

**Testing.** Two layers:

1. **Pure unit tests** for guards (`validProxyMeta`, `validCreds`,
   `validExternal`) — each is just a function over context.
2. **React Testing Library integration tests** that mount the modal,
   mock the TanStack mutations, and click through the three critical
   paths:
   - Happy create (proxy path, end-to-end through to `result.success`).
   - Edit (proxy path, `metadata → updating → result.success`).
   - Partial-failure rollback (`creatingProxy` fails →
     `rollingBackEnv` succeeds → back at `credentials` with error).

Skip XState's model-based testing here — the wizard is linear enough
that auto-generated path coverage isn't worth the setup cost.

**Behavior parity check before merging:**

- All four flows: external create, proxy create, proxy edit (audience
  unchanged), proxy edit (audience changed).
- Pre-fill from discovery on both paths.
- Failure modes: invalid JSON, mutation 4xx, env-name collision suffixing.
- **New:** proxy creation failure rolls back the environment.
- **New:** rollback-failure lands in `fatalError` (terminal) — the user
  sees a compound error and a Close button, not a retry path.
- Modal close animation: content stays put for the duration of the close
  animation (no flash back to path selection).
- Modal close mid-flight: in-flight `creatingEnvironment` is aborted, or
  if it isn't abortable, no orphan env is created (verify via mocked
  network).
- Modal close + reopen on both create and edit (the "key" remount path).

## 7. Cost / value

- **Add deps:** `xstate` (~25kb gzipped), `@xstate/react` (small). Already
  used in many React projects; v5 is stable.
- **LOC:** roughly net-neutral. We delete `reducer.ts` (170 lines) +
  `actions.ts` (~290) + `types.ts` (80), and add `machine.ts` (~250) +
  `guards.ts` (~60) + `services.ts` (~100).
- **Win:** the flow is readable in one file; mutation chains and loading
  states become first-class; impossible states become impossible. New
  async steps drop in as one state + one guard + one service rather
  than threading pending booleans through forms.
- **Risk:** team unfamiliarity with xstate v5. Mitigation: this is the only
  machine in the dashboard right now, so the blast radius is contained — if
  it doesn't pay off we revert in isolation.

## 8. Decisions

Resolved on 2026-04-28:

1. **Scope.** xstate is introduced for this one wizard. We're not waiting
   for a second use case. If it doesn't pay off here we revert in
   isolation.
2. **Authoring.** Code-first. No Stately Studio dependency.
3. **Partial-failure handling.** Roll back the environment when
   `creatingProxy` fails. Modeled as the explicit `rollingBackEnv` state
   in §4. Rollback failure → terminal `fatalError` state (do **not**
   route back to `credentials`; retrying would create a duplicate env).
4. **Modal close timing.** Keep the existing close-animation delay. The
   machine remount is gated behind that delay so content doesn't reflow
   mid-animation (see §4.5).
5. **Credential rotation.** Not supported by either the wizard or the
   edit modal. Editing only mutates metadata fields.
6. **Edit lives outside the FSM.** Editing an existing OAuth proxy is
   a separate `EditOAuthProxyModal` — a plain form +
   `useUpdateOAuthProxyServerMutation`. The wizard machine is
   create-only. Reason: edit is one form → one PUT; the machine's value
   (mutation cascade, partial-failure rollback, multi-step navigation,
   `pathSelection`/`credentials` substates) doesn't apply, and folding
   edit into the FSM dilutes both flows.
7. **Free-tier gate.** Stays outside the machine, as the
   `ConnectOAuthModal` wrapper does today.

## 9. Prerequisites for the PR

- [x] Delete-environment endpoint exists (`/rpc/environments.delete`,
      hard delete, idempotent, `project:write` scope) and the client
      already has `useDeleteEnvironmentMutation`. No server work needed.
- [ ] Add `xstate` v5 + `@xstate/react` to `client/dashboard/package.json`;
      note bundle-size impact in the PR description.
- [ ] Confirm whether the create/update/delete environment and proxy
      mutations accept an `AbortSignal` (needed for clean cancellation
      on modal close — see §4.5).
