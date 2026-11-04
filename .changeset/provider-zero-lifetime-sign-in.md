---
"server": patch
---

A sign-in where the provider reports a zero, negative or unreadable lifetime now works instead of showing as expired straight away or failing. On a direct server with no credential choice, "Give access" now refuses a posted approval, as well as disabling the button, while every service on the page still needs connecting or reconnecting, and the page says so. When Verify cannot get a working sign-in, it now returns to the service on the page, which says to reconnect or try again, and its log says why.
