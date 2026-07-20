---
"server": minor
---

Widen the assistant runtime's message representation from plain strings to structured text|image content parts, end to end through the Go runtime and the Rust runner. Message content is now a string-or-parts union on both sides of the runner wire protocol (back-compat in both directions), history replay prefers the structured content captured at store time over the plain-text projection, turn requests gain an optional `input_parts` field, and chat persistence strips inline `data:` image bytes to text placeholders before anything is written at rest. Behavior-neutral groundwork: nothing produces image parts yet.
