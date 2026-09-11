---
"dashboard": patch
"server": patch
---

The setup board gains a Set up LiteLLM card, hidden by default until a platform admin reveals it. Its page creates a LiteLLM instance in place, shows that instance's proxy environment, guardrail fragment, and verification requests exactly as the AI Integrations page does, and confirms traffic from the instance's connection diagnostics. LiteLLM guardrail events no longer count toward the other-platforms card.
