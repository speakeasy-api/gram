---
"server": minor
"dashboard": minor
---

Policy URL-list edits can no longer contradict recorded MCP access decisions silently. Editing an already-blocking shadow MCP policy's allow or block list now reviews the change against the project's standing decisions: unchecking an approved server, allow-listing a denied one, block-listing an approved one, or unblocking a denied one is refused with a conflict unless the save explicitly confirms superseding those decisions (`supersede_decisions` on `risk.updatePolicy`). A confirmed save transitions each displaced review to the new `superseded` status — actor-attributed, audit-logged (`mcp_approval_request:supersede`), decision history and rationale preserved — and the policy replay and drift recheck stop deriving enforcement from it until someone re-decides. Ordinary re-saves also stop rewriting decision-written grant audiences with the policy audience: a scoped approval's blast radius now survives unrelated policy edits. The dashboard policy editor shows a confirmation dialog listing the contradicted servers before such a save, and superseded reviews render with their own badge and inventory filter.
