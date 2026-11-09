# Chat collection access contract

This document records what each chat collection path reveals today, so that later authorization work changes those answers only by explicit decision. It is the parity contract that chat lists must keep when their checks move to typed constructors. It covers listings, counts, source and link lookups, and summaries. Point reads (opening one chat) are included where they differ.

`server/internal/chat/chat_collection_contract_test.go` runs the per-caller table for `ListChats` (page, total and past-the-end count), `ListSources`, `LoadChat` and `LoadChatOverview`, and the unauthorized case for `ListSessionLinks` too. Link masking and assistant summaries are pinned by the existing tests named below. Changing any answer below must change one of those tests, and the change must be listed in the PR as a behaviour change.

## How collections decide today

Every collection path goes through `chatVisibilityScope` in `server/internal/chat/impl.go`. It returns three SQL inputs:

- `external_user_id`: when set, only that external user's sessions.
- `user_id`: when set, only that user's sessions, plus the agent sessions described next.
- `owned_agent_ids`: agent sessions that count as the caller's own. A session counts only if it has no `user_id`, an empty `external_user_id`, and an `agent_id` in this list. The list holds the caller's agents in the organization that are not deleted and not latched for owner reassignment.

Both identity inputs empty means every session in the project.

The scope is chosen in this order:

1. **External callers.** An `ExternalUserID` on the auth context always restricts the scope to that external user. Any payload filter is ignored.
2. **Project-wide readers.** All sessions are visible, narrowed only by the optional `external_user_id` and `user_id` payload filters, in two cases:
   - RBAC is enforced and the project-level chat read (`authz.ChatReadCheck(projectID)`, evaluated with `Evaluate`) allows.
   - The caller is the managed-assistant runtime.
3. **Everyone else** is restricted to their own sessions.
   - With no user ID, the request is refused as unauthorized.
   - Summaries leave the caller's owned-agent sessions out (`humanSessionsOnly`). All other paths include them (`withOwnedAgentSessions`). This applies only to own scope: a project-wide reader's summary has no identity filter, so it counts agent sessions like any other.

The project-level check is `Evaluate`, not `Require`, because this is a visibility branch and not a gate. Neither of the following fails the request: both fall back to own sessions. The check only feeds step 2, so an external caller stays on its external user, and the managed-assistant runtime stays project-wide, whatever the check returns.

- **RBAC not enforced** (`ShouldEnforce` false): collections stay on own sessions. They do not treat the skipped check as allowed.
- **Check errors** (`ShouldEnforce` or `Evaluate` returns an error): the error is logged and the request continues.

## Per-operation matrix

| Operation                        | Queries                                                                                                 | Agent sessions                                                       | Errors                                                            | Snapshot                                                                                                               |
| -------------------------------- | ------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------- | ----------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------- |
| `ListChats` page                 | `ListChats`                                                                                             | included                                                             | visibility error: own scope; owned-agent lookup error: unexpected | the total is a window count in the page statement; past the first page, the page runs in a repeatable-read transaction |
| `ListChats` past-the-end total   | `CountChats`                                                                                            | included                                                             | as above                                                          | same transaction as the page it follows                                                                                |
| `ListChats` Slack channel column | `ListChatSlackChannels`                                                                                 | included                                                             | as above                                                          | read after the page, outside the transaction                                                                           |
| `ListSources`                    | `ListChatSources`                                                                                       | included                                                             | as above                                                          | single statement                                                                                                       |
| `ListSessionLinks`               | `ListChatSessionLinks`                                                                                  | included                                                             | as above                                                          | single statement                                                                                                       |
| `GetAssistantSessionSummary`     | `GetAssistantSessionSummaryProjection`, `ListAssistantSessionSummaryChats`                              | own scope: owned-agent sessions **excluded**; project-wide: included | needs `project:read` first (hard gate, forbidden)                 | one statement each, no shared snapshot                                                                                 |
| `LoadChatOverview`               | point gate, then `ListChats` by ID with no identity filter, then `ListChatSlackChannels` with the scope | n/a                                                                  | point-read errors                                                 | separate statements                                                                                                    |
| `LoadChat` Slack channel         | `ListChatSlackChannels` with the scope                                                                  | included                                                             | as above                                                          | separate statement                                                                                                     |

`ListSessionLinks` also masks link ends for restricted callers. Unrestricted means both identity inputs are empty. A restricted caller sees an end's chat ID only when that end is captured and in scope.

The owned-agent list is resolved before the page statement or transaction. So the page and its total share one snapshot, but the agent list may come from an earlier one.

## Collections compared with point reads, per caller

"Own scope" means the caller's own human sessions plus sessions of agents they own (the default for a restricted caller).

| Caller                                                                | Collections reveal                    | `LoadChat` and `LoadChatOverview` open | Refused point reads return |
| --------------------------------------------------------------------- | ------------------------------------- | -------------------------------------- | -------------------------- |
| `chat:read` on every resource                                         | every session                         | every session                          | none refused               |
| `chat:read` naming the project ID                                     | every session                         | own scope only                         | forbidden                  |
| `chat:read` naming one chat                                           | own scope                             | own scope plus that chat               | forbidden                  |
| member with no chat grants                                            | own scope                             | own scope                              | forbidden                  |
| enforced, visibility check errors (not the managed-assistant runtime) | own scope                             | own scope                              | unexpected                 |
| direct legacy API key (RBAC not enforced)                             | key creator's own scope               | every session                          | none refused               |
| chat-session token minted by an API key                               | the embedded external user's sessions | the embedded external user's sessions  | unauthorized               |
| chat-session token minted by a dashboard session                      | the embedded external user's sessions | the dashboard user's own scope         | forbidden                  |
| managed-assistant runtime                                             | every session                         | every session                          | none refused               |
| own scope with no user ID                                             | refused (unauthorized)                | not covered                            |                            |

Edge cases the test fixture covers:

- An anonymous session (no user and no external user) is visible only to project-wide readers.
- An owned agent stamped on another user's session does not make that session the agent owner's. It stays with the human it carries.
- A foreign agent's session is never in the caller's own scope.

These existing tests cover three more cases:

- An agent latched for reassignment drops out of its owner's scope (`TestListChats_Member_LatchedAgentSessionsHidden`).
- `ListSessionLinks` masks link ends (`TestListSessionLinks_MemberVisibilityScoped`).
- Assistant summaries need project read and stay inside the assistant (`assistant_session_summary_test.go`).

## Contract for later list work

Content-read authority and metadata discovery are separate questions, and stay separate:

- **Point reads** (content-read authority) ask `authz.ChatReadCheck(chatID)` or `authz.ChatWriteCheck(chatID)` through `authorizeChatAccess`, after the service decides ownership.
- **Collections** (metadata discovery) ask one project-level question, `authz.ChatReadCheck(projectID)`, and otherwise apply the own-scope predicate.

A collection must not be rewritten as "the chats a point read would open". It stays its own operation: list the chats visible to that project-level read or to the own-scope predicate.

Any rewrite of that operation (new constructors, SQL changes) must reproduce:

1. Project-wide when the project-level read allows, or when the caller is the managed-assistant runtime.
2. The external-user restriction first, ignoring payload filters.
3. The own-scope predicate exactly as written above. That includes the human-identity precedence over `agent_id`, and the latched and deleted agent exclusion.
4. Owned-agent sessions left out of own-scope assistant summaries, and kept in project-wide ones.
5. Own scope, not failure, when the project-level check errors or RBAC is not enforced.
6. Unauthorized when the own scope has no user ID.
7. Predicates applied before pagination and counting, with the page and total read from one repeatable-read snapshot.

**Expected improvement:** resolving owned agents inside that snapshot (a join or subquery instead of a prefetched ID list) is allowed. It narrows a race and does not change any answer above.

## Open decisions

These paths keep their current behaviour. No change to them ships until someone decides:

1. **A `chat:read` grant naming the project lists every session but opens none.** The point operation carries the chat ID with no project dimension, so a project-named grant never matches it. Making point reads honour the project would widen content access, which is a product decision.
2. **A grant on one chat opens it but never lists it.** Listing grant-named chats would widen discovery.
3. **A direct legacy API key lists only its creator's sessions but opens every session.** Collections treat "RBAC not enforced" as restricted, while point reads treat it as allowed.
4. **A chat-session token minted by a dashboard session lists the embedded external user's sessions but opens the dashboard user's.** Collections follow `ExternalUserID`, while point reads skip the external-user gate whenever a dashboard session is present.
5. **A failed visibility check degrades collections to own scope, but makes point reads fail as unexpected.**

None of these is a regression. The test pins each one so that any change is visible.
