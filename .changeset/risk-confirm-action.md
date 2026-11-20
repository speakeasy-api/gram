---
"server": minor
"dashboard": minor
---

The risk policy action "warn" is now called "confirm": it blocks the prompt or tool call until the user confirms they want to go ahead, so the user can bypass it themselves and every confirmation is audited. The API returns `confirm` and still accepts `warn` as a deprecated alias, and existing warn policies keep working unchanged. The dashboard labels the action "Block until confirmed", explains who can get past each action, and shows confirm outcomes as "Awaiting confirmation", "Confirmed by user" and "Not confirmed".
