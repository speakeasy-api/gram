---
tags: [skill-creator]
max_turns: 80
timeout_seconds: 3600
allowed_tools: [Read, Glob, Grep, Skill, Write, Edit, Bash]
---

We are adding tenant provisioning and I want it on the sc statechart package. Provisioning creates a database, then runs migrations on it, then registers a DNS record. Each step calls an external API that can fail. If migrations or DNS fail we have to delete the database we created. An operator can abort a provisioning that is still in progress. Rules: a tenant that ends up failed or aborted never keeps a database (no orphans); we never register DNS for a database that has been or will be deleted; a tenant only becomes ready after all three steps succeeded. Please model this before building it, then implement the chart with tests. I am away, so pick sensible assumptions and note them.
