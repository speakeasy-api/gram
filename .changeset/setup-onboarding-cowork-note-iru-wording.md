---
"dashboard": patch
"server": patch
---

Surface that Claude Cowork still needs its own manual setup step when Device
Agent is selected on the "Instrument agents" onboarding step — Device Agent
only covers coding assistants running on the developer's machine, not
Cowork's cloud sandbox. The new note links straight into the Manual Setup
flow for Cowork.

Also aligns MDM vendor wording with the Iru rebrand ("Iru (formerly Kandji)")
across the Device Agent setup page and Codex onboarding copy, matching the
naming already used on the MDM integrations page.

Conversation events (`UserPromptSubmit`/`Stop`) are now also written to
ClickHouse telemetry so the onboarding "Confirm traffic" feed shows prompts
and assistant replies, not only tool calls.
