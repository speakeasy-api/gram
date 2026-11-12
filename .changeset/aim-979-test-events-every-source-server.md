---
"server": minor
---

Every data export can now send a test event, not just MCP connection logs. Tool call logs, risk findings and MCP connection logs send one test log. Product telemetry sends one test log, one trace and one metric, and the answer lists each one. Every test log, span and metric data point, and every request's resource, carries `speakeasy.test_event=true`. The test log and span also carry a `speakeasy.test_event.id` that is the same for every signal in one test; the metric does not, so tests add no metric time series. Every test log is named `speakeasy.test_event` and its message starts with "Speakeasy test event:". Speakeasy drops `speakeasy.test_event` and `speakeasy.test_event.id` from data it receives, so real data never looks like a test. The answer and the audit log include the test event ID. The Platform MCP tool `send_data_export_test_event` now asks for the route's data source as well as its destination.

A product telemetry test adds one span under service `speakeasy` and one metric named `speakeasy.test_event` to your destination.
