---
"server": minor
---

Assistant completions now route through a project's own model provider key when one covers the assistants slot. Projects without a key keep the current platform-covered behavior. The key slot a completion uses is derived from the authenticated caller rather than request headers.
