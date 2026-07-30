---
"dashboard": patch
---

Fix the agent-coverage meter reading as far more filled than its real percentage in dark mode. The track used `bg-muted`, which collapses to the card color in dark mode, hiding the uncovered remainder so the filled portion looked much larger than its true share. The track now uses a foreground tint that stays visible on both light and dark grounds.
