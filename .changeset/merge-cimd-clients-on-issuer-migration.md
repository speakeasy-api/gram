---
"server": patch
---

Consolidating a user session issuer onto another no longer stops when the same app is registered on both through its client metadata document, as Claude Code and Codex are. The source registration merges into the target's: sessions and consents move to it, a person who approved the same access through both issuers keeps the target's approval, and resources approved only through the source are added to it. A shared registration still blocks the migration when it was registered dynamically, or when an issuance selection profile ties it to the source issuer.
