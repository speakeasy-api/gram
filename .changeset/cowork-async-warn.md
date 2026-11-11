---
"server": minor
---

Warn policies now reach Claude Cowork users. Cowork runs no per-event hooks, so a warn policy could not stop a prompt before the model saw it, and matches only appeared later in the Watchdog. When `speakeasy-hooks` ships a Cowork transcript from Stop, the server now checks the newly shipped user prompts against the project's enforcing policies and returns the warn matches. The binary hands them back to Claude from the same Stop, so Claude tells the user about the warning in the conversation they just had. The prompt has already been sent, so the warning is a notice: nothing is blocked and no acknowledgement is needed. Block and quarantine policies are still not enforced inside Cowork.
