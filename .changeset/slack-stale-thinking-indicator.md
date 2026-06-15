---
"server": patch
---

fix(slack): suppress the ingress "thinking" indicator for ambient events. Plain channel messages, reactions, and other passive Slack events that may end in a silent turn no longer light up the loading indicator, which previously stranded it until Slack's two-minute timeout. Only events the assistant always replies to (@-mentions, DMs, Block Kit interactions) show the indicator.
