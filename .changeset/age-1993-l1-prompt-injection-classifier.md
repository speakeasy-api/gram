---
"server": patch
"dashboard": patch
---

add an opt-in L1 ML prompt-injection classifier (deberta-v3) that runs alongside the heuristic baseline. enable the new "ML classifier (deberta-v3)" rule under the Prompt Injection category in the policy editor to layer the classifier on top of L0 heuristics. detection runs in a sidecar service; configure with `PI_CLASSIFIER_URL` and `PI_CLASSIFIER_THRESHOLD` (default `0.9`)
