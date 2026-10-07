---
"server": patch
---

agent_events rows now carry the model, the tool, the skill and agent, the words, the outcome and its message, and the duration from the OTel transform step, and the new `name` column is filled with the subject of a tool event, so every consumer of the normalized record sees what happened and the catalog can count tools, skills and outcomes without re-reading each producer's own attributes.
