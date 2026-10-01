---
"server": minor
"admin": minor
---

The support matrix is now code: a single file in the server, embedded in the binary and changed by pull request, states every cell explicitly, meaning for each integration method on each platform whether it applies, which account types can use it, what is known per operating system, and one status per capability. The admin dashboard's Support matrix page shows the matrix the server was built with, read-only, with a link to the file on GitHub; the in-page editor, the CSV import and the save endpoint are gone, and the CSV export stays. The Admin MCP's support matrix tool reads the same file.
