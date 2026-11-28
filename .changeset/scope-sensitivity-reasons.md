---
"server": patch
---

The scope catalogue declares four more reasons a scope can be sensitive: `credentials`, `member_identity`, `member_activity` and `access_control`. No scope carries them yet. They are declared first so clients know them before the scopes that replace `org:admin` start using them.
