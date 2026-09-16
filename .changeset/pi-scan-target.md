---
"server": patch
---

Shadow AI now detects Pi (pi.dev) on enrolled devices. The scan target catalog gains Pi as a coding harness, matched by its `pi` binary and process name and by its `~/.pi` configuration directory, so a device running Pi is reported instead of coming back clean. Pi ships no MCP client of its own — servers reach it only through third-party extensions that register them as ordinary Pi tools — so it joins as a detection-only target: it publishes no client identity document, and a decision about it is recorded but not enforceable.
