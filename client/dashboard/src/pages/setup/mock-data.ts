import type {
  IdpProvider,
  DirectoryUser,
  AgentPlatform,
  McpSource,
  TrafficMetric,
} from "./types";

export const IDP_PROVIDERS: IdpProvider[] = [
  {
    id: "okta",
    name: "Okta",
    icon: "O",
    type: "SAML 2.0 / OIDC",
    connected: false,
  },
  {
    id: "azure",
    name: "Microsoft Entra ID",
    icon: "M",
    type: "SAML 2.0 / OIDC",
    connected: false,
  },
  {
    id: "google",
    name: "Google Workspace",
    icon: "G",
    type: "OIDC",
    connected: false,
  },
  {
    id: "onelogin",
    name: "OneLogin",
    icon: "1",
    type: "SAML 2.0",
    connected: false,
  },
  {
    id: "jumpcloud",
    name: "JumpCloud",
    icon: "J",
    type: "SAML 2.0 / OIDC",
    connected: false,
  },
];

export const MOCK_DIRECTORY_USERS: DirectoryUser[] = [
  { id: "1", name: "Sarah Chen", email: "sarah.chen@acme.com", role: "admin" },
  {
    id: "2",
    name: "Marcus Johnson",
    email: "marcus.j@acme.com",
    role: "admin",
  },
  {
    id: "3",
    name: "Emily Rodriguez",
    email: "e.rodriguez@acme.com",
    role: "member",
  },
  { id: "4", name: "David Kim", email: "d.kim@acme.com", role: "member" },
  {
    id: "5",
    name: "Lisa Thompson",
    email: "l.thompson@acme.com",
    role: "member",
  },
  { id: "6", name: "James Wilson", email: "j.wilson@acme.com", role: "member" },
  {
    id: "7",
    name: "Anna Martinez",
    email: "a.martinez@acme.com",
    role: "member",
  },
  { id: "8", name: "Michael Brown", email: "m.brown@acme.com", role: "member" },
];

export const AGENT_PLATFORMS: AgentPlatform[] = [
  {
    id: "cursor",
    name: "Cursor",
    description: "AI-powered code editor",
    icon: "cursor",
    connected: false,
  },
  {
    id: "windsurf",
    name: "Windsurf",
    description: "Codeium IDE",
    icon: "windsurf",
    connected: false,
  },
  {
    id: "claude-desktop",
    name: "Claude Desktop",
    description: "Anthropic desktop app",
    icon: "claude",
    connected: false,
  },
  {
    id: "vscode",
    name: "VS Code + Copilot",
    description: "GitHub Copilot integration",
    icon: "vscode",
    connected: false,
  },
  {
    id: "jetbrains",
    name: "JetBrains IDEs",
    description: "IntelliJ, PyCharm, etc.",
    icon: "jetbrains",
    connected: false,
  },
];

export const MCP_SOURCES: McpSource[] = [
  {
    id: "github",
    name: "GitHub",
    type: "1st-party",
    description: "Code repositories and issues",
    enabled: false,
  },
  {
    id: "slack",
    name: "Slack",
    type: "1st-party",
    description: "Team messaging and channels",
    enabled: false,
  },
  {
    id: "linear",
    name: "Linear",
    type: "1st-party",
    description: "Issue tracking and projects",
    enabled: false,
  },
  {
    id: "notion",
    name: "Notion",
    type: "1st-party",
    description: "Documents and wikis",
    enabled: false,
  },
  {
    id: "jira",
    name: "Jira",
    type: "3rd-party",
    description: "Project management",
    enabled: false,
  },
  {
    id: "confluence",
    name: "Confluence",
    type: "3rd-party",
    description: "Documentation platform",
    enabled: false,
  },
  {
    id: "datadog",
    name: "Datadog",
    type: "3rd-party",
    description: "Monitoring and analytics",
    enabled: false,
  },
  {
    id: "sentry",
    name: "Sentry",
    type: "3rd-party",
    description: "Error tracking",
    enabled: false,
  },
];

export const MOCK_TRAFFIC_METRICS: TrafficMetric[] = [
  { label: "Active Users", value: "24", trend: "up", healthy: true },
  { label: "Tool Requests", value: "1,247", trend: "up", healthy: true },
  { label: "Blocked Calls", value: "38", trend: "down", healthy: true },
  { label: "Compliance Rate", value: "96.8%", trend: "stable", healthy: true },
];
