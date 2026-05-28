import type {
  DirectoryUser,
  AgentPlatform,
  McpSource,
  TrafficMetric,
} from "./types";

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
    id: "claude",
    name: "Claude Code",
    description: "Anthropic CLI & IDE agent",
    icon: "claude",
    connected: false,
    setupSteps: [
      {
        title: "Install the Speakeasy hook",
        description:
          "Add the Speakeasy hooks package to your Claude Code configuration.",
        code: `claude mcp add speakeasy -- npx -y @speakeasy-api/hooks@latest`,
        language: "bash",
      },
      {
        title: "Configure your API key",
        description:
          "Set the Speakeasy API key so the hook can authenticate requests.",
        code: `export SPEAKEASY_API_KEY="sk_live_speakeasy_xxxxxxxxxxxxx"`,
        language: "bash",
      },
      {
        title: "Verify the connection",
        description:
          "Start a Claude Code session and confirm hook output appears in your Speakeasy dashboard.",
      },
    ],
  },
  {
    id: "codex",
    name: "OpenAI Codex",
    description: "OpenAI CLI agent",
    icon: "codex",
    connected: false,
    setupSteps: [
      {
        title: "Install the Speakeasy wrapper",
        description: "Add Speakeasy as a wrapper around Codex CLI invocations.",
        code: `npm install -g @speakeasy-api/codex-hooks@latest`,
        language: "bash",
      },
      {
        title: "Configure environment",
        description:
          "Set environment variables for Codex to route through Speakeasy.",
        code: `export SPEAKEASY_API_KEY="sk_live_speakeasy_xxxxxxxxxxxxx"\nexport SPEAKEASY_CODEX_HOOK=true`,
        language: "bash",
      },
      {
        title: "Verify the connection",
        description:
          "Run a Codex session and confirm traffic appears in the Speakeasy dashboard.",
      },
    ],
  },
  {
    id: "cursor",
    name: "Cursor",
    description: "AI-powered code editor",
    icon: "cursor",
    connected: false,
    setupSteps: [
      {
        title: "Open Cursor settings",
        description:
          "Navigate to Cursor Settings > MCP and add a new global MCP server.",
        code: `{\n  "mcpServers": {\n    "speakeasy": {\n      "command": "npx",\n      "args": ["-y", "@speakeasy-api/hooks@latest"]\n    }\n  }\n}`,
        language: "json",
      },
      {
        title: "Add your API key",
        description:
          "Set the Speakeasy API key in your environment or Cursor settings.",
        code: `export SPEAKEASY_API_KEY="sk_live_speakeasy_xxxxxxxxxxxxx"`,
        language: "bash",
      },
      {
        title: "Verify the connection",
        description:
          "Open Cursor and confirm the Speakeasy MCP server appears as connected.",
      },
    ],
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
