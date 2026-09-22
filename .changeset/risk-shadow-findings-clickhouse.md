---
"server": patch
---

Organizations running the `shadow` risk engine mode now keep the fine-tuned risk model's verdicts in the findings store, marked so they can be compared with the legacy engines' findings per message. Shadow findings are never enforced and never appear in Risk Events, the Dismissed listing, the overview, signals, the Watchdog or reveal.
