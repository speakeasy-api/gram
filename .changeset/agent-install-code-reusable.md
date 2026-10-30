---
"server": patch
"dashboard": patch
---

An agent's one-line setup command can now be run more than once until its code expires, 15 minutes after it was generated. Before, the first fetch spent the code, so reading the script first or retrying a failed install needed a new command. The setup wizard and the agent key page now say the code expires in 15 minutes instead of calling it single-use.
