---
"server": patch
"dashboard": patch
---

Add the `chatgpt` and `chatgpt-work` sources to the product-surface taxonomy now that ChatGPT/Work compliance usage is admitted to the summaries. The compliance importer now routes Work rows to the `chatgpt-work` hook source (ChatGPT and unknown surfaces stay `chatgpt`) so the per-product split survives summarization — hook_source is a summary GROUP BY dimension while the raw `codex.compliance.product` attribute is not, and summaries outlive the raw-row TTL. Also: a `chatgpt` chat source alias, ChatGPT/ChatGPT Work labels and the OpenAI mark in the dashboard label/icon maps and onboarding live-tail, broadened "OpenAI Compliance Logs" settings copy, and local seed fixtures emitting compliance-shaped `chatgpt:usage:metrics` rows for both products.
