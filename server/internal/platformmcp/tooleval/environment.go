package tooleval

// Environment describes the fictional organization every case talks about.
// The agent reads it in its system prompt, so cases can name these resources
// and expect their IDs and slugs in arguments without a lookup first. Every
// identifier is made up; none belongs to a customer.
const Environment = `You are connected to Speakeasy as Dana Lee (dana@acme.example), an organization administrator of Acme Robotics.

Known resources (use these exact IDs and slugs when a tool needs them):
- Organization: Acme Robotics, slug acme-robotics.
- Projects: "Default" (slug default, id 0b7c3d1e-0000-4000-8000-000000000001) and "Payments" (slug payments, id 0b7c3d1e-0000-4000-8000-000000000002).
- MCP servers in Default: "GitHub" (slug github, id 5f1e2a00-0000-4000-8000-000000000011) and "Linear" (slug linear, id 5f1e2a00-0000-4000-8000-000000000012).
- MCP servers in Payments: "Billing API" (slug billing-api, id 5f1e2a00-0000-4000-8000-000000000021), built from an OpenAPI document.
- Organization gateway: "Acme Gateway" (slug acme-gateway, id 7a9d0c00-0000-4000-8000-000000000031).
- Plugin: "Engineering" (slug engineering, id 3c4b5a00-0000-4000-8000-000000000041), assigned to the Engineering directory group.
- Skill: "release-notes" (id 9e8d7c00-0000-4000-8000-000000000051).
- Risk policy: "Secrets in chats" (id 2d3e4f00-0000-4000-8000-000000000061).
- Data export: "Splunk SIEM" (id 4e5f6a00-0000-4000-8000-000000000071).
- Dashboard: "Weekly MCP usage" (id 6f7a8b00-0000-4000-8000-000000000081).
- Members: Dana Lee (dana@acme.example, admin), Sam Ortiz (sam@acme.example, member), Priya Shah (priya@acme.example, member).`
