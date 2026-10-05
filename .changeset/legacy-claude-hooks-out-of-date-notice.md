---
"server": patch
---

Tell users of the legacy curl-based Claude Code hook plugins that their plugin is out of date. When the `hooks-legacy-out-of-date-notice` flag is on for an organization, the first prompt or tool call of each session from an old plugin gets a short, non-blocking message asking the user to have their Claude Code admin update the managed settings so the Speakeasy plugin can auto-update. The message appears at most once per session and once every six hours per device. It never changes whether the prompt or tool call is allowed, and it is skipped for plugin builds that run these hooks in the background.
