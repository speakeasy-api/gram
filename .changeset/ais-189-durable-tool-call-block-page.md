---
"server": minor
"dashboard": minor
---

Tool-call blocks are now durable, first-class entities with a stable `/blocks/<id>` URL and 👍/👎 feedback. When the risk engine blocks a tool call, the block is persisted and its reason is injected into the agent-facing response (Claude `PermissionDecisionReason`, Cursor `AgentMessage`, Codex `reason`) along with a link to the block page, so the agent can reason about the denial instead of hallucinating one. New session-scoped, org-admin-gated `getRiskBlock` and `submitRiskBlockFeedback` endpoints back an in-app `BlockDetailPage` (under `AppLayout`) and a slug-free redirect resolver for the agent's external link, with a "More Info" link from the Risk Events modal.
