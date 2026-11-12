---
"server": minor
---

Org admins can send a test event to an MCP connection logs export's destination, from the management API (`dataExports.testRoute`) or the Platform MCP (`send_data_export_test_event`). Speakeasy sends one event, marked as a test, straight to the destination and says in plain words whether it was accepted, refused, not found, or could not be reached. Each export can send 5 test events a minute, and each organization 20 across all its exports. If Speakeasy can't check these limits, it sends nothing and asks you to try again shortly. Each test is recorded in the audit log.
