---
"server": minor
---

Org admins can send a test event to an MCP connection logs export's destination, from the management API (`dataExports.testRoute`) or the Platform MCP (`send_data_export_test_event`). Speakeasy sends one event, marked as a test, straight to the destination and says in plain words whether it was accepted, refused, not found, or could not be reached. Each test is rate-limited per export and recorded in the audit log.
