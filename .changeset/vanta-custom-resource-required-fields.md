---
"server": patch
---

Fix three Vanta evidence-sink defects against the real CustomResource API, all verified live. Every pushed record now carries the required top-level `externalUrl` base field (an omission was rejected with 400). `agent_last_seen_at` is always sent — an empty string when no agent has ever reported, rather than omitted — because Vanta's console cannot author an optional-property schema, so a device-declared record schema marks every property required and an omitted field fails at sync. And the response check now matches Vanta's actual full-state PUT contract — 200 `{"success": true}` on a valid set, 4xx on any schema violation — instead of requiring an `accepted`/`rejected` accounting object the API never returns, which was failing every push.
