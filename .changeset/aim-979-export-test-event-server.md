---
"server": minor
---

Org admins can send a test event to any data export's destination, from the management API (`dataExports.testRoute`) or the Platform MCP (`send_data_export_test_event`). Speakeasy sends the test straight to the destination and says in plain words whether it was accepted, refused, not found, or could not be reached. Tool call logs, risk findings and MCP connection logs send one test log. Product telemetry sends one test log, one trace and one metric, and the answer lists each one.

Test events never look like real data. Every test log, span and metric data point, and every request's resource, carries `speakeasy.test_event=true`. The test log and span also carry a `speakeasy.test_event.id` that is the same for every signal in one test; the metric does not, so tests add no metric time series. Every test log is named `speakeasy.test_event` and its message starts with "Speakeasy test event:". Speakeasy drops `speakeasy.test_event` and `speakeasy.test_event.id` from data it receives, so real data never looks like a test. A product telemetry test adds one span under service `speakeasy` and one metric named `speakeasy.test_event` to your destination.

Each export can send 5 tests a minute, and each organization 20 across all its exports. If Speakeasy can't check these limits, it sends nothing and asks you to try again shortly. Each test, with its test event ID, is recorded in the audit log.
