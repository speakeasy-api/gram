---
"server": patch
---

Fix project-assistant thread titles all rendering identically. Each thread's
chat was seeded with the assistant's name, which the async title generator
treats as a deliberately-chosen title and refuses to overwrite (its guard only
replaces recognized sentinel defaults). Threads are now seeded with the
`"New Chat"` placeholder so the generator activates and produces a unique title
per thread from the first turn's content.
