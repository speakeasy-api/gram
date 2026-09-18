---
"server": patch
---

Anthropic inference hooks now store and evaluate only what each delivery adds to the conversation, plus the current turn. Rolling compaction in Claude no longer causes the transcript to be archived again, and long conversations no longer time out and deny requests because every earlier turn was re-scanned on each call.
