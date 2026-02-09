---
"@gram-ai/elements": minor
---

Chart plugin and generative UI overhaul

**Chart Plugin**
- Replace Vega-Lite with Recharts for React 19 compatibility
- Add themed tooltips using CSS variables (oklch colors)
- Update chart stories to use MCP orders summary tool

**Generative UI**
- Add macOS-style window frames with traffic light buttons
- Add whimsical cycling loading messages (50 messages, 2s fade transitions)
- Streamline LLM prompt from ~150 lines to concise bulleted format

**Component Fixes**
- ActionButton executes tools via useToolExecution hook
- Align Text, Badge, Progress props with LLM prompt specification
- Fix catalog schema toolName → action mismatch
- Fix setTimeout cleanup in CyclingLoadingMessage

**Storybook**
- Fix theme toggle causing full component remount
