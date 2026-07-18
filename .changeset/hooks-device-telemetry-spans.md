---
"server": patch
"hooks": minor
---

Hook traces now begin on the device. The speakeasy-hooks binary starts the trace for each hook invocation and reports on-device telemetry — operating system, architecture, binary build, coding-agent harness, and on-device elapsed time — which the server stamps onto hook endpoint spans so hook performance can be measured end to end and issues diagnosed per platform.
