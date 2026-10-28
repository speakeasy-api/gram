# Person delegation for OpenClaw

DNO-1414 and DNO-1428. Research against main at `77f8fe7a6f` and the description
of PR #8168 at `4423a2eb3cbe7cd36e3c97d9b2489f18c68f3345`.

## Finding: distinguish credential authorizer from message sender

The ticket's statement that an agent key represents no person is too broad for
the current implementation. `classifyPrincipalAPIKey` in
`server/internal/auth/key.go` sets a principal-backed key's authorizer from its
stored `CreatedByUserID`. `server/internal/agents/runtimepolicy/credential_admission.go`
loads that authorizer's live policy, and `server/internal/authz/credential_admission.go`
binds `$represented_user` to this trusted authorizer. The agent's current owner is
a separate policy boundary; the implementation does not simply substitute the
current owner for the authorizer.

This is a credential-level identity, not proof that the person who just messaged
OpenClaw requested a particular action. A shared agent key cannot identify a
different sender on each turn. Adding a sender name, email, or user ID to a tool
argument would not establish that proof.

Other credential shapes differ. Legacy API keys use only their own grants
(`server/internal/authz/api_key_policy.go`); workload admission explicitly leaves
the represented user empty (`server/internal/authz/workload_admission.go`). A
design must name the credential shape instead of treating every agent runtime as
equivalent.

## Two independent capabilities

| Capability              | Meaning                                                  | Required evidence                                                                               |
| ----------------------- | -------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| Preassigned connection  | A person permits an agent to use a connection in advance | Authenticated owner consent, exact connection binding, live permissions and revocation          |
| Per-turn representation | A particular authenticated person requested this turn    | Verified sender-to-user mapping, bounded delegation for that turn, isolation from other senders |

PR #8013 addresses connection assignment during agent creation. PR #8168 extends
explicit assignment and renewal into MCP authentication. Neither should be
described as proving the sender of an arbitrary OpenClaw message.

## Gateway ticket reconciliation

Main's `agentAuthorizationTarget` in
`server/internal/mcp/authnchallenge_consent_agent.go` deliberately rejects a
gateway target: its old client-as-agent flow cannot represent and revalidate the
member resource set. Removing that guard alone would remove a safety boundary.

The active #8168 design follows a different flow: the MCP client signs in as the
human, then the human explicitly grants selected agents the upstream connection.
For gateways and multiple upstream services, it performs this optional assignment
after each callback. Its description states exact callback session/generation
binding, ownership rechecks, stale-grant rejection, and partial-success reporting.
Those are claimed capabilities of the active PR, not validation performed by this
research change.

DNO-1428 should therefore be reviewed against #8168 before implementing another
picker. Multiple independent upstream providers still require their own sign-in
and consent; a gateway entry point cannot collapse those into one provider login.
The desired outcome is a coherent gateway flow with explicit per-service agent
assignment, not restoring the old client identity selector.

## Proposed per-turn protocol

1. A trusted integration verifies the incoming channel event and resolves its
   immutable sender identity to an eligible organization user. Treat display
   names and agent-supplied attribution as untrusted metadata.
2. That integration obtains a short-lived delegation bound to the user, agent,
   tenant, intended resource/audience, and turn. A normal agent key alone must
   not authorize minting a delegation for arbitrary people.
3. Carry this delegation in a dedicated credential channel. Do not put it in
   model-visible prompts, tool arguments, logs, or a shared global configuration.
4. Admission intersects the requested authority with live agent, owner, and
   delegator permissions. Connection selection uses only the explicitly verified
   identity and permitted assignments. Never fall back to an owner's connection
   when sender verification or authorization fails.
5. Isolate credentials across concurrent turns, tool calls, retries, and delayed
   work. An unattended scheduled turn requires its own explicit standing
   delegation; it cannot reuse the last interactive sender.

The concrete event verifier, issuer, token format, replay policy, and OpenClaw
transport hook remain design work. DNO-971's attribution is relevant input but
cannot substitute for authentication. AIM-412's existing per-turn delegation is
a reference to inspect before implementing another issuance path.

## Acceptance cases

- Two people use the same agent concurrently; neither sees or uses the other's
  connection, including when one user lacks a required connection.
- Forged sender metadata, wrong audience/tenant, replayed or expired delegation,
  disabled user, revoked agent permission, and stale callback are rejected.
- Gateway membership changes between initiation and approval cannot widen the
  approved resource set. Partial provider success is reported accurately.
- Background retries preserve the original authorized identity only within its
  validity; they never inherit the next sender's credentials.
- Audit events distinguish agent actor, credential authorizer, verified turn
  sender where available, and selected connection owner.

This PR changes no admission behavior. The immediate implementation recommendation
is to reconcile DNO-1428 with #8168 and design a separately authenticated per-turn
path for DNO-1414 rather than accepting a caller-supplied represented-user field.
