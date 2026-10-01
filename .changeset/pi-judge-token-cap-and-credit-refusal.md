---
"server": patch
---

Cap the prompt-injection and prompt-policy judges at 8192 completion tokens so OpenRouter stops refusing them with 402 when a key's remaining credit is below the model's full output ceiling. Credit refusals and truncated completions now fail open as `insufficient_credits` and `truncated` instead of generic errors.
