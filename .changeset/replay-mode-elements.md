---
"@gram-ai/elements": minor
---

Add replay mode and cassette recording for Elements. The `<Replay>` component plays back pre-recorded conversations with streaming animations — no auth, MCP, or network calls required. The `useRecordCassette` hook and built-in composer recorder button (gated behind `VITE_ELEMENTS_ENABLE_CASSETTE_RECORDING` env var) allow capturing live conversations as cassette JSON files.
