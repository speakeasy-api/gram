---
"server": minor
---

Risk policies gain granular application: an `application_config` `{ include,
exempt }` predicate set narrows which messages a policy evaluates (include
supersedes the now-deprecated `message_types`) and exempts matched messages
inline, and `exempt_rule_ids` attaches custom rules as allowlist exemptions. The
real-time and batch scan paths gate detection on include/exempt before any
source runs (and skip the judge for out-of-scope/exempt messages); the `risk`
API and SDK expose both on policy create/update.
