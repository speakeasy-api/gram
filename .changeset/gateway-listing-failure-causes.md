---
"server": patch
---

A gateway member that can't list its tools now says why, the same way a failed tool call on it does: the upstream rejected a saved key, shared credential or person's connection, rate limited, had an error or didn't answer, its private connection is down, its connection isn't set up correctly, or something failed on our side. The gateway's connect page shows the cause and who can fix it, and connection health records it, naming the member's upstream host only when the upstream itself failed. A malformed tool listing still reads as a member whose tools are missing.

Provider health counts a person's listing and call failures on one gateway member during one outage once.
