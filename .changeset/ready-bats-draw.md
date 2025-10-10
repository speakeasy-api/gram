---
"@gram/server": patch
---

feat: introduces “healing” of invalid tool call arguments. LLMs sometimes malform inputs sometimes incorrectly stringify for complicated schemas even when the schema definition is correct. We can unpack the correct json object out of this, even after the LLM mistake.
