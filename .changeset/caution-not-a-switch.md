---
"dashboard": patch
"server": patch
---

Registering a workload issuer no longer asks whether wildcard admission is allowed. The contract defaults it to on, and the admit dialog states the consequence where a wildcard is actually written instead — naming the subjects the rule admits and the agent they would inherit. Whether a wildcard is sound is a judgement about the operator's own platform, and it is better served at the point of decision than by a setup-time question asked before they have a rule in mind. The issuer field remains in the API, where clearing it still makes every wildcard rule under that issuer inert immediately.
