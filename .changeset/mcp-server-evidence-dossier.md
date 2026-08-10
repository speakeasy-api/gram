---
"server": minor
---

Server evidence decouples from asking: mcpApproval.ensureServerReview resolves the evidence dossier for any server URL, opening one in a new unreviewed status when none exists. Dossiers stay out of the decision queue and upgrade in place when someone actually requests the server, so evidence can be inspected before — or without — any review being decided.
