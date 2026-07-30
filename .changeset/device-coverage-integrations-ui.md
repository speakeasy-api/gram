---
"dashboard": minor
---

Restructure the Device Agent MDM integrations page around the source → coverage → destination pipeline. A pipeline banner shows live connected counts (updating as connections are enabled/disabled) and the org-wide fleet coverage, over two role-labeled groups. Detail pages are now role-specific: inventory sources keep their device inventory and "synced" language, while evidence destinations drop the inventory table (a sink owns no devices), show what they publish org-wide, use "pushed" language, and surface a "Fleet sourced from" breakdown linking to the sources that feed them.
