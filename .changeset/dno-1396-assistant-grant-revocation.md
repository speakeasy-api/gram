---
"server": patch
---

Removing every assistant permission from the built-in Admin or Member role now revokes assistant access for its members. Effective permissions always match the role's stored grants; organizations created before assistant permissions existed receive their stored defaults through a one-time backfill.
