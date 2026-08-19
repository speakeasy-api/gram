---
"server": minor
---

Add the per-user filters an identity view needs: member grants, plugin assignments, policy challenges, and bypass requesters.

Several subsystems could only answer "who holds this" and not "what does this person hold", so there was no way to assemble a per-person view of access. This adds the missing direction:

- `GET /rpc/access.listMemberGrants` returns another member's effective grants, direct and role-inherited. It is a separate method from `listGrants` because every carve-out in that one — scope overrides, impersonation, the demo org, pre-organization sessions — is about the caller, and none apply when the subject is somebody else. Requires `org:admin`.
- `plugins.listPlugins` takes `principal_urns`, narrowing the listing to the plugins those principals receive. Plugins distributed to everyone are included: from a principal's side, an org-wide plugin is one they get.
- `GET /rpc/risk.listPolicyChallenges` lists the warn/challenge history for a set of user identifiers. The table has always been indexed for this; nothing exposed it.
- `risk.listPolicyBypassRequests` takes `requester_user_ids`.

The two endpoints that take a set of identifiers do so because one person is commonly recorded under several, and the one on a given row is whichever the agent reported at the time.
