---
"dashboard": minor
"server": patch
---

Project assistant tool calls now render Claude-style: the assistant precedes each tool batch with a terse activity phrase ("Investigating failures in the last 30 days") which becomes the heading of a single collapsed tool group. Consecutive batches merge into one group whose heading advances (with shimmer) as the investigation progresses, groups never auto-expand, and the global thinking loader hides while a tool group is streaming. The dashboard output-channel guidance instructs the model to emit the phrase before every tool call.
