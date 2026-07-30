---
"server": patch
---

Fix the Vanta evidence sink against the real CustomResource schema, verified live. Every pushed record now carries the required top-level `externalUrl` base field (an omission was rejected with 400), and `agent_last_seen_at` is always sent — an empty string when no agent has ever reported, rather than omitted — because Vanta's console cannot author an optional-property schema, so a device-declared record schema marks every property required and an omitted field fails at sync.
