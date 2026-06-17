---
"server": patch
---

Slack-connected assistants now decide whether a reply adds value before posting: ambient thread messages can be answered with silence, while @-mentions always get a reply. The `platform_slack_set_thread_status` tool accepts an empty status to clear the thread's loading indicator on silent turns.
