---
"server": minor
---

Members without `log:read` can now read the tool-call logs and traces of their own sessions and of the agents they own. The four raw log endpoints narrow to that activity instead of refusing, so an agent's owner can troubleshoot it without a grant that reveals every member's tool arguments and results.
