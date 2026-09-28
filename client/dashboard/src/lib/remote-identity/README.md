# `lib/remote-identity`

Models, hooks, and components for one idea: **a remote identity provider is a
complete, working configuration for signing into an upstream service** — not
the two or three records we happen to store it in.

It also owns the alternative to that: the static credential an administrator
sets up when nobody is signing in as themselves (Agent Identity), and the
absence of both (No Identity). Those three are one choice, so they live
together.

## The modelling claim

The API exposes three records for what an administrator experiences as one
thing:

| Record                                       | What it really is                   |
| -------------------------------------------- | ----------------------------------- |
| `remote_session_issuers`                     | where users sign in                 |
| `remote_session_clients`                     | the OAuth client registered there   |
| `remote_session_client_user_session_issuers` | that client bound to one MCP server |

Nobody configuring a server thinks in those terms, and AIM-230 says so
outright: _"Administrators experience the upstream provider and client as one
Remote Identity Provider configuration; issuer, client, DCR, and CIMD
vocabulary stays out of the linear server setup flow."_

So this module owns the rules for reading them as one thing. Code that needs
the underlying records still gets them — the provider CRUD pages legitimately
edit an issuer as an issuer — but no feature surface should have to assemble
the idea for itself, and none should have to learn the vocabulary to ask a
simple question.

The module deliberately stops at the rules. An earlier draft also exported a
composed `RemoteIdentityProvider` record and a `Capabilities<C>` wrapper; both
went unused, because the surfaces that need a provider need the issuer and the
client separately anyway. They are gone rather than kept as speculative API.

```ts
/** The whole choice a server's Identity section makes. */
type UpstreamIdentity =
  | { mode: "user"; binding: RemoteIdentityBinding }
  | { mode: "agent"; credential: AgentCredential }
  | { mode: "none" };
```

`UpstreamIdentity` is derived, never stored. A server reads as `user` because a
client is bound to its user session issuer, and as `agent` because a static
`Authorization` header exists. Keeping the derivation here is what stops three
surfaces from each re-deriving it, which is what they used to do.

## Layout

```
lib/remote-identity/
  index.ts                  public surface, re-exports from below

  model/                    pure. no React, no network
    identity.ts             IdentityMode and its derivation
    headers.ts              Authorization rules, ManagedHeader
    provider.ts             capabilities, tier, display names
    credential.ts           AgentCredential <-> Authorization header
    secret.ts               Secret: { unchanged } | { set, value }

  drafts/                   editing server state
    draft.ts                Draft<T>, reconcile, DraftError
    headerDrafts.ts         one header row, and the rules for writing it
    useHeaderDrafts.ts      the header rows, as one editable list
    useIdentityDraft.ts     the provider and client choice
    useCredentialDraft.ts   the static credential

  queries/                  server reads
    useAllRemoteSessionClients.ts  every client for one user session issuer
    useProtectedResourceMetadata.ts  what an upstream URL advertises
    useUpstreamProbe.ts     does this URL demand authentication
    useClientSessions.ts    has anyone actually signed in

  components/               the shared UI of the concept
    IdentityModeCards.tsx   User / Agent / No Identity
    ProviderRow.tsx         the provider and registration choice
    CredentialFields.tsx    format, fields, live Authorization preview
    IdentityExplainer.tsx   content, dialog, and inline callout
    ProviderLink.tsx        deep link to the CRUD surface
    ScopeBadge.tsx          tier as a badge
```

`model/` has no React import. `drafts/` and `queries/` have no JSX. That split
is the point: the derivation rules are testable without rendering, and the
components stay free of query wiring.

## What does not live here

- **Page composition.** `RemoteMcpIdentitySection` arranges these pieces into
  a settings panel; it stays in `pages/`.
- **Provider CRUD.** The sheets and pages under
  `pages/remote-identity-providers/` edit an issuer _as an issuer_. They
  consume `model/` but keep their own forms — AIM-230 deliberately keeps the
  complex OAuth surface there, out of the linear setup flow.
- **Header row markup.** `HeadersSection` still draws the rows. It is
  presentational now: the state, the validation and the write are
  `useHeaderDrafts`, so the identity panel's Save can commit rows and identity
  together.

## The rule the module exists to enforce

The identity choice **owns** the `Authorization` header: Agent Identity writes
one, User Identity forbids one, No Identity rejects the name outright. That
ownership flows one direction only.

```ts
type ManagedHeader = {
  ownedBy: "user" | "agent";
  headerId: string | null; // null: owned, not written yet
};
```

`HeadersSection` used to call the derivation itself and re-find the managed row
— the same computation the identity section had already done, from a second
fetch that nothing kept in step. One owner publishes it now, and the rows
neither validate that row nor write it.

## Drafts

Everything editable here is server state you are changing, so it follows one
shape: a `baseline`, a working copy, and `isDirty` as a **comparison against
the baseline** — never a flag meaning "was touched". `isValid` is separate,
because Save needs both answers.

Every draft answers _what will Save do_ before Save is pressed, which is what
the panel needs to render one button: `isDirty` and `isValid` decide whether it
is live, and comparing the selected mode against the derived one decides
whether it needs a confirmation first.

Probe results are deliberately **not** part of a draft. They decide which
options exist and whether the form can be submitted, never whether it changed,
so they stay outside the draft shape rather than becoming more fields on it.

The full lifecycle — including what happens when a probe answers late, or the
baseline moves under a dirty draft — is written up on AIM-230.

## Migration

Done. The table records where each piece came from.

| Moves from                                            | To                                        |
| ----------------------------------------------------- | ----------------------------------------- |
| `…/authentication/remoteMcpIdentity.ts`               | `model/identity.ts`                       |
| `…/authentication/useUserIdentityDraft.ts`            | `drafts/useIdentityDraft.ts`              |
| `…/authentication/useAgentCredentialDraft.ts`         | `drafts/useCredentialDraft.ts`            |
| `…/authentication/useAllRemoteSessionClients.ts`      | `queries/useAllRemoteSessionClients.ts`   |
| `…/authentication/useProtectedResourceMetadata.ts`    | `queries/useProtectedResourceMetadata.ts` |
| `…/authentication/useRemoteMcpAuthenticationProbe.ts` | `queries/useUpstreamProbe.ts`             |
| `…/authentication/identityModes.tsx`                  | `components/IdentityModeCards.tsx`        |
| `…/authentication/IdentityExplainer.tsx`              | `components/IdentityExplainer.tsx`        |
| `…/authentication/AgentIdentityRow.tsx`               | `components/CredentialFields.tsx`         |
| `…/authentication/UserIdentityRow.tsx`                | `components/ProviderRow.tsx`              |
| `pages/remote-identity-providers/clientDisplay.ts`    | `model/provider.ts`                       |
| `pages/remote-identity-providers/IssuerLink.tsx`      | `components/ProviderLink.tsx`             |
| `pages/remote-identity-providers/ScopeBadge.tsx`      | `components/ScopeBadge.tsx`               |
| `remoteSessionScopeTier` in `lib/sources.ts`          | `model/provider.ts`                       |

`RemoteIdentitySummary` stayed in `components/mcp-server-x-sidebar-nav.tsx`.
It is a rail-shaped composition of this module's parts rather than a part of
its own, and it now reads the derivation from here instead of reaching five
directories deep into a settings tab for it.

## How it landed

The moves were mechanical and noisy, so they did not ride along with behaviour
changes:

1. `model/` and `secret.ts` first — pure, no consumers to break, unblocking the
   `Secret` union that headers and the credential form both need.
2. `queries/`, one hook per server read the concept needs.
3. `drafts/`, where the dirtiness bugs were fixed rather than relocated:
   `useIdentityDraft` compared values instead of recording an interaction, and
   `useCredentialDraft` stopped reading validity as dirtiness.
4. `components/`, one at a time, each a pure move with its tests.
5. `ManagedHeader`, then one Save: identity and header rows now commit
   together, and the Custom Headers disclosure no longer carries its own
   button.

Still deferred: `Secret` reaching the wire, and the provider CRUD pages under
`pages/remote-identity-providers/`.

## A convention note

The dashboard's frontend guidance puts shared components in
`src/components/`. This module keeps its components beside its models on
purpose: they are meaningless without the concept, and splitting them would
recreate the reaching-across-directories problem in a new place. Settled
deliberately, against the convention, and worth revisiting if a second feature
ever wants these components without the concept.
