---
"@gram-ai/elements": patch
---

fix: resume the chat turn after a client-side frontend tool completes. `useChatRuntime` now wires `sendAutomaticallyWhen: lastAssistantMessageIsCompleteWithToolCalls`, so Skip/Save clicks on frontend-tool forms no longer leave the conversation stuck with an unresolved tool-call.
