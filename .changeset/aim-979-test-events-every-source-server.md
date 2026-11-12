---
"server": minor
---

Every data export can now send a test event, not just MCP connection logs. Tool call logs and risk findings send one test log. Product telemetry sends one test log, one trace and one metric, and the answer lists each one. Every test record, span and metric data point carries `speakeasy.test_event=true` and a `speakeasy.test_event.id` that is the same for every signal in one test, and each request's resource carries `speakeasy.test_event=true` too. Log messages start with "Speakeasy test event:", and log event names are test names, such as `gram.risk.finding.test`, so a test never looks like real data. The answer and the audit log include the test event ID. The Platform MCP tool `send_data_export_test_event` now asks for the route's data source as well as its destination.
