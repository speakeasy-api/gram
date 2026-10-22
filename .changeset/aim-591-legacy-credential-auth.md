---
"server": patch
---

Agent keys, agent sessions and workload sessions whose stored policy predates version 5 are read at admission with an auth client ceiling matching each server they can connect to, still bounded by the agent's, owner's and authorizer's live permissions. Nothing changes for requests today; the mandatory authorization shadow now evaluates these credentials as the upcoming auth client check will, and labels them `legacy_auth_ceiling`. Nothing is re-issued.
