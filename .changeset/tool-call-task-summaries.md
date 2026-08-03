---
"server": minor
"dashboard": minor
---

Present the project assistant's tool activity as a short, human-readable "task" line — e.g. "Pulling the last 30 days of usage" — instead of a mechanical "Calling N tools" header, with the individual tool calls collapsed behind it.

The label is the assistant's own words: models narrate a turn before acting, and that sentence already arrives in the same message immediately before the tool calls. The chat renders it as the group's heading rather than as separate prose above it, so the description appears the instant the calls are dispatched and costs no extra model call. The managed assistant's instructions ask for that line in heading form. A group with no such line falls back to naming the activity from its tool names.
