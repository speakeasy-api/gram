---
"dashboard": patch
"server": patch
---

Sign-ups are now attributed to the campaign that brought the visitor. The sign-up page forwards its UTM parameters and analytics session to the login request, and the server attaches them, with the sign-up host, to the first-time sign-up and new organization events.
