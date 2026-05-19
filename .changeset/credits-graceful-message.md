---
"@gram-ai/elements": minor
"dashboard": patch
---

Show a graceful message in AI Insights and the Playground when an organization runs out of chat credits. Previously the chat would silently stop streaming on a 402 from the gateway because the AI SDK masks stream errors by default. The thread now renders `You've reached the chat credit limit for this account. Click the "Get Support" button at the top of the page to reach out about upgrading.` and the new `describeStreamError` / `CREDITS_EXHAUSTED_MESSAGE` exports are available on `@gram-ai/elements` for downstream consumers that want to react to the same condition.
