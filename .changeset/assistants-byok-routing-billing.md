---
"server": minor
---

Assistant completions now route through a project's own model provider key when one covers the assistants slot, and that customer-covered assistant usage is counted as tokens under management on the billing page. Projects without a key keep the current platform-covered behavior. The key slot a completion bills to is now derived from the authenticated caller rather than request headers.
