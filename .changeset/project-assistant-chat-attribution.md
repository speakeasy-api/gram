---
"@gram-ai/elements": minor
"dashboard": patch
---

Attribute project assistant chats to the user who created them. Elements gains an opt-in `history.resolveCreator` callback that resolves a chat's `userId`/`externalUserId` to a displayable name/avatar, shown on thread-list rows and above each user turn in the transcript. The dashboard wires this up for the Project Assistant using its existing org member list — no new network requests, and no identity data is fetched from inside Elements itself (avoids leaking org member data into customer-facing embeds, which don't opt in). Also adds the same avatar to the "Recent Chats" list on the assistant home page, and gives user message bubbles an iMessage-style blue treatment.
