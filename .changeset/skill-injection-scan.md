---
"server": minor
---

Scan captured and authored skill manifests for prompt-injection content as part of the skills pipeline. A scheduled Temporal sweep runs unscanned skill versions through the existing prompt-injection judge and records the verdict (`injectionFlagged` / `injectionRationale`) on the version, which the dashboard surfaces as a warning banner on the skill detail page.
