---
"server": patch
---

Admin dashboard sessions and staff Admin MCP connections now expire after 48 hours of inactivity, and admin sessions end 45 days after sign-in however active they stay. Refreshing a staff Admin MCP token restarts the inactivity timeout, so a connection in regular use no longer needs daily consent. Every use still validates the staff member's session live, and each write still needs browser approval.
