---
"server": patch
---

Prompt-based policies now take `judge_temperature` and `judge_fail_open` as top-level fields instead of nesting them under `model_config`. Updating one judge setting no longer resets the other.
