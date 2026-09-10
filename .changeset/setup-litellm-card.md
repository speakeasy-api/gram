---
"dashboard": patch
"server": patch
---

The setup board gains a Set up LiteLLM card. Its page creates a LiteLLM instance in place, walks through the proxy environment, guardrail fragment, and verification requests using the same snippet builders as the AI Integrations page, and confirms traffic as LiteLLM guardrail events arrive. LiteLLM also joins the platform picker in the instrumentation sheet, and its events no longer count toward the other-platforms card.
