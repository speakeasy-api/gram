---
"server": minor
"dashboard": patch
---

The Watchdog findings KPI now follows the selected time range. The tile was
hardwired to the trailing 24 hours ending at the window's edge, so picking a
different range with the date picker left the number unchanged while every
other tile updated. The riskSignals result now reports window-scoped
`findings` / `previous_findings` (replacing `findings_24h` /
`previous_findings_24h`), computed from the same deduplicated window counts
the risk score already used, and the tile compares against the equal-length
previous period like its neighbors.
