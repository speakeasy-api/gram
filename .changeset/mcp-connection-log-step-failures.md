---
"server": patch
"dashboard": patch
---

MCP connection log exports set to send failed and blocked connections now also get failed steps a person or app must fix while the connection is still going, such as an expired sign-in. They arrive as warnings with `final=false`, since the person may still recover. Normal sign-in steps are not included. Exports set to failed connections only are unchanged.
