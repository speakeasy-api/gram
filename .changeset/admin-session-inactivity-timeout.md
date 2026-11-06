---
"server": patch
---

Admin dashboard sessions and staff Admin MCP connections now expire after 48 hours of inactivity. Refreshing a staff Admin MCP token restarts the timeout, so a connection in regular use no longer needs daily consent. Every use still validates the staff member's session live, and each write still needs browser approval.
