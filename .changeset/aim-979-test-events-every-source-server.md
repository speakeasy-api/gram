---
"server": minor
---

Every data export can now send a test event, not just MCP connection logs. Tool call logs and risk findings send one test log. Product telemetry sends one test log, one trace and one metric, and the answer lists each one. Every test record carries `speakeasy.test_event=true` and a test event name, such as `gram.risk.finding.test`, so it never looks like real data. The Platform MCP tool `send_data_export_test_event` now asks for the route's data source as well as its destination.
