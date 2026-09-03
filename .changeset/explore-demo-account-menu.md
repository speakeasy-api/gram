---
"dashboard": patch
---

Fix the "Explore demo org" item in the account menu doing nothing when clicked. Route helpers silently stubbed out `goTo` for absolute-path routes such as `/explore-demo`, so the click closed the menu and stayed on the current page. Absolute routes now navigate the same way every other route does.
