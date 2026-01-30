---
"@gram-ai/elements": patch
---

Fix logs page performance, responsive charts, tool output rendering, and streaming indicator

- Memoize config objects and callbacks in Logs page and thread to prevent unnecessary re-renders
- Fix tool group count using startIndex/endIndex instead of filtering all message parts
- Fix shimmer CSS in shadow DOM by setting custom properties on .gram-elements
- Auto-size charts to container width via ResizeObserver instead of fixed 400px minimum
- Truncate large tool output to 50-line preview, skip shiki for content over 8K chars
- Show pulsing dot indicator after tool calls while model is still running
