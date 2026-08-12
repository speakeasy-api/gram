---
"server": patch
"dashboard": patch
---

Make custom model provider keys (BYOK) generally available. The `custom_model_keys` product feature is now always on for every organization: `IsFeatureEnabled` short-circuits it to true, `getProductFeatures` always reports it enabled, disable attempts are no-ops, and the Internal Admin toggle is removed. The feature name remains in the API for compatibility.
