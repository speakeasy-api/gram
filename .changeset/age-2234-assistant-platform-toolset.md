---
"server": minor
---

Every assistant now exposes a platform toolset to its runtime alongside its user-attached toolsets, with no user-facing toolset row and no setup required. Removes the `assistant_memory` product feature flag in the process: `GET /rpc/productFeatures.get` no longer returns `assistant_memory_enabled`, and `POST /rpc/productFeatures.set` no longer accepts `"assistant_memory"` as a `feature_name` — the assistant memory tools are always-on.
