---
"server": patch
---

Updated the cursor format on /rpc/deployments.logs endpoint to be based on off of the sequential ID of the deplayment logs rather than the UUID of the log entry. This ensures a strong ordering of logs in the presence of multiple logs created at the same timestamp.

This problem was pronounced when processing Gram Functions and external MCP servers that would create batches of of logs with overlapping timestamps, leading to out-of-order logs in the API response.
