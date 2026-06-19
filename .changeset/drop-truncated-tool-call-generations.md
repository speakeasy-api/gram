---
"server": patch
---

fix(assistants): stop assistant threads from getting stuck when a model response is cut off mid-tool-call. A truncated generation used to be saved with malformed tool-call arguments, which made the thread fail and retry forever (silent assistants, wedged cron digests). Such generations are now dropped at capture while the preceding messages are kept, so the thread stays usable.
