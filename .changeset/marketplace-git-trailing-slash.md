---
"server": patch
---

Fix marketplace git proxy returning 404 when the git source URL has a trailing
slash. Claude Code's managed settings appends a trailing slash to git URLs;
the proxy now strips it before routing so `TOKEN.git/info/refs` resolves
correctly.
