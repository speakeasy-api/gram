---
"server": minor
---

Assembles the evidence document at approval-request intake. Every admission now gathers the deterministic signals — resolved identity, package-registry metadata, and the org's own traffic exposure — into the versioned document the approval surface renders and decisions freeze. Found, not-published, and could-not-look are distinct outcomes, per-source failures are recorded as gaps inside the document rather than read as clean, and a flaky registry can delay evidence but never lose an admission. The optional research agent stays deliberately outside this document: web-sourced findings keep their own trust tier and lifecycle, and the deterministic document doubles as the agent's briefing when an admin later asks for a run.
