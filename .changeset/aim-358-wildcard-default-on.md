---
"server": patch
---

`workload_issuers.allow_wildcard_admission` now defaults to on. Whether a wildcard rule is sound is a judgement about the operator's own platform, and it is better served by stating the consequence where a wildcard is actually written than by a setup-time gate asked before they have a rule in mind. The column stays because it is checked on every lookup rather than at write time, so clearing it makes every wildcard rule under that issuer inert immediately — an incident control rather than a configuration step.
