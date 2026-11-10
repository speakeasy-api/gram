---
"server": minor
"admin": minor
"dashboard": patch
---

Add a staff MCP debugger to the admin dashboard's MCP Registry. A new staff-only `admin.debugMcpUrl` endpoint traces a remote MCP server step by step from Speakeasy's network: it reaches the server, reads its authentication challenge, fetches protected resource and authorization server metadata, detects how clients register (client metadata document, dynamic registration, or a pre-registered client), and, when no sign-in is needed, initializes a session and lists its tools, prompts and resources. It sends no credentials and stores nothing. Staff can run it on any registry entry's remote from the entry's Debug action, or on any URL from "Debug a URL". When a server expects a client metadata document that Speakeasy's own address cannot serve, such as on a local stack, the trace warns that connecting will fail. Once a trace reaches the server, "Verify in Speakeasy" opens the customer dashboard in Speakeasy's own project so staff can sign in and run its tools in the Playground. If that project already has a server for the URL, the button opens its Playground. Otherwise it opens the add-server page with the URL filled in, which now accepts `url` and `name` query parameters. The link comes from a read-only `admin.getMcpVerificationLink` endpoint; nothing is added until staff save the form.
