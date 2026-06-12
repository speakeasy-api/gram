---
"server": patch
---

Replace the Slack assistant's rotating loading indicator with honest, single-phrase status.

The thread indicator no longer cycles through a fake "Routing… → Calling tools… →
Composing…" pipeline. On ingress it shows just "Routing…", and once the assistant is
running it reports what it's actually doing through the set-thread-status tool — one
phrase at a time, updated as the work progresses. The tool now also instructs the
model to phrase the status mid-sentence (Slack renders it after the app's name) and
pins the indicator to the status text when no loading message is given, instead of
letting Slack rotate its own generic defaults.
