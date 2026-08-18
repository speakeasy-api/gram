---
"server": minor
"dashboard": patch
---

MCP approval change detection and re-review: a daily sweep re-gathers evidence for approved servers and compares the permission-relevant slice (OAuth scopes, authority mode, demanded credentials, published advisories) against the snapshot the approval rested on. Drift sets a changed-since-approval flag — cleared only by a new decision — announces once per distinct change through the audit-log webhook channel, and surfaces on the review page as a diff banner and on the inventory as a badge.
