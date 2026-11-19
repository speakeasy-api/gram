---
"server": patch
---

When a gateway member can't list its tools, its card on the connect page now says, in everyday words, whether the server is busy, had an error, didn't answer, has its sign-in service down, had its saved sign-in details rejected, has its sign-in set up wrongly or has its private network tunnel down, and whether to wait or ask an administrator; other causes keep the generic card. Connection health records the same cause a tool call on that member would, naming the member's upstream host only when the upstream itself failed. Running out of the gateway listing's own time still reads as a member whose tools are missing. Listing rows never count toward provider health.
