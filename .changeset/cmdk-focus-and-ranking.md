---
"dashboard": patch
---

Fix two Cmd+K command palette glitches. Recently Visited is now only shown when the search box is empty, so a closer text match always ranks ahead of a recently visited page (AGE-2808). The "Ask AI" row drops below the results while searching, so the closest match keeps the auto-selected highlight instead of requiring an extra ↓ keypress to reach it (AGE-2807).
