/**
 * Mock data model for the Context CMS (Docs MCP on Gram).
 *
 * The tree is made up of folders, markdown files, skill files (SKILL.md),
 * and mcp-docs.json config files that carry taxonomy / chunking metadata.
 */

// ── Types ──────────────────────────────────────────────────────────────────

export type DocsMcpConfig = {
  version: "1";
  strategy?: {
    chunk_by: "h1" | "h2" | "h3" | "file";
    max_chunk_size?: number;
    min_chunk_size?: number;
  };
  metadata?: Record<string, string>;
  taxonomy?: Record<
    string,
    {
      vector_collapse: boolean;
      properties?: Record<string, { mcp_resource: boolean }>;
    }
  >;
  mcpServerInstructions?: string;
  overrides?: Array<{
    pattern: string;
    strategy?: DocsMcpConfig["strategy"];
    metadata?: Record<string, string>;
  }>;
  /** Role-based access control on taxonomy areas. */
  accessControl?: Array<{
    role: string;
    allowedTaxonomy?: Record<string, string[]>;
    deniedPaths?: string[];
  }>;
};

export type ContextFileKind = "markdown" | "skill" | "mcp-docs-config";

export type FileVersion = {
  version: number;
  updatedAt: string;
  author: string;
  message: string;
  size: number;
};

export type DraftLayer = {
  content?: string;
  config?: DocsMcpConfig;
  updatedAt: string;
  author: string;
};

export type Annotation = {
  id: string;
  author: string;
  authorType: "human" | "agent";
  content: string;
  createdAt: string;
};

export type DocFeedback = {
  upvotes: number;
  downvotes: number;
  labels: string[];
  userVote?: "up" | "down" | null;
};

export type ContextFile = {
  type: "file";
  name: string;
  kind: ContextFileKind;
  content?: string;
  config?: DocsMcpConfig;
  updatedAt: string;
  size: number;
  versions: FileVersion[];
  draft?: DraftLayer;
  /** Source provenance. */
  source?: "manual" | "cli" | "agent" | "github";
  annotations?: Annotation[];
  feedback?: DocFeedback;
};

export type ContextFolder = {
  type: "folder";
  name: string;
  children: ContextNode[];
  updatedAt: string;
};

export type ContextNode = ContextFile | ContextFolder;

// ── Skills registry types ─────────────────────────────────────────────────

export type RegistrySkill = {
  id: string;
  name: string;
  description: string;
  body: string;
  source: "corpus" | "captured" | "uploaded";
  /** For captured skills: which agent session captured it. */
  capturedFrom?: {
    sessionId: string;
    agentName: string;
    capturedAt: string;
  };
  status: "active" | "pending-review" | "disabled";
  author: string;
  path?: string;
  updatedAt: string;
  invocations: number;
  frontmatter: Record<string, string>;
};

// ── Observability types ───────────────────────────────────────────────────

export type SearchLogEntry = {
  id: string;
  query: string;
  filters?: Record<string, string>;
  resultsCount: number;
  topChunkPath: string;
  latencyMs: number;
  sessionId: string;
  agentName: string;
  timestamp: string;
};

export type SkillInvocationEntry = {
  id: string;
  skillId: string;
  skillName: string;
  sessionId: string;
  agentName: string;
  latencyMs: number;
  timestamp: string;
  success: boolean;
};

// ── Capture settings ──────────────────────────────────────────────────────

export type CaptureSettings = {
  enabled: boolean;
  captureProjectSkills: boolean;
  captureUserSkills: boolean;
  ignoreWithFrontmatter: boolean; // x-gram-ignore
};

export const MOCK_CAPTURE_SETTINGS: CaptureSettings = {
  enabled: true,
  captureProjectSkills: true,
  captureUserSkills: false,
  ignoreWithFrontmatter: true,
};

// ── Version history helper ────────────────────────────────────────────────

function makeVersions(
  count: number,
  latestDate: string,
  latestSize: number,
): FileVersion[] {
  const authors = ["alice", "bob", "carol", "dave"];
  const messages = [
    "Initial draft",
    "Fix typos and formatting",
    "Add missing section",
    "Update examples",
    "Restructure content",
    "Add code samples",
    "Editorial review",
  ];
  const versions: FileVersion[] = [];
  const base = new Date(latestDate).getTime();
  for (let i = count; i >= 1; i--) {
    const daysBack = (count - i) * 3;
    versions.push({
      version: i,
      updatedAt: new Date(base - daysBack * 86400000).toISOString(),
      author: authors[(count - i) % authors.length],
      message: messages[(count - i) % messages.length],
      size: Math.max(100, latestSize - (count - i) * 30),
    });
  }
  return versions;
}

// ── Mock data ──────────────────────────────────────────────────────────────

export const MOCK_CONTEXT_TREE: ContextFolder = {
  type: "folder",
  name: "docs",
  updatedAt: "2026-03-28T10:00:00Z",
  children: [
    {
      type: "file",
      name: ".docs-mcp.json",
      kind: "mcp-docs-config",
      config: {
        version: "1",
        strategy: {
          chunk_by: "h2",
          max_chunk_size: 12000,
          min_chunk_size: 200,
        },
        metadata: { product: "gram", scope: "public-docs" },
        taxonomy: {
          language: {
            vector_collapse: true,
            properties: {
              typescript: { mcp_resource: true },
              python: { mcp_resource: true },
              go: { mcp_resource: false },
            },
          },
        },
        mcpServerInstructions:
          "This server provides Gram product documentation. Use the search tool to find relevant guides and API references.",
        overrides: [
          {
            pattern: "guides/advanced/*.md",
            strategy: { chunk_by: "h3" },
            metadata: { scope: "advanced-guide" },
          },
        ],
        accessControl: [
          {
            role: "developer",
            allowedTaxonomy: {
              language: ["typescript", "python", "go"],
            },
          },
          {
            role: "support",
            allowedTaxonomy: { language: ["typescript", "python"] },
            deniedPaths: ["guides/advanced/*"],
          },
          {
            role: "external-partner",
            allowedTaxonomy: { language: ["typescript"] },
            deniedPaths: ["api-reference/*", "guides/advanced/*"],
          },
        ],
      },
      updatedAt: "2026-03-27T15:30:00Z",
      size: 842,
      versions: makeVersions(3, "2026-03-27T15:30:00Z", 842),
      source: "manual",
    },
    {
      type: "folder",
      name: "getting-started",
      updatedAt: "2026-03-25T09:00:00Z",
      children: [
        {
          type: "file",
          name: "introduction.md",
          kind: "markdown",
          content:
            "# Introduction\n\nWelcome to Gram! This guide walks you through the basics of setting up your first MCP server.\n\n## Prerequisites\n\n- Node.js 18+\n- A Gram account\n\n## Quick Start\n\n```bash\nnpx create-gram-app my-server\ncd my-server\nnpm run dev\n```\n\nYour MCP server is now running locally on port 3000.",
          updatedAt: "2026-03-20T12:00:00Z",
          size: 312,
          versions: makeVersions(4, "2026-03-20T12:00:00Z", 312),
          draft: {
            content:
              "# Introduction\n\nWelcome to Gram! This guide walks you through the basics of setting up your first MCP server.\n\n## Prerequisites\n\n- Node.js 20+\n- A Gram account\n- pnpm 9+\n\n## Quick Start\n\n```bash\nnpx create-gram-app my-server\ncd my-server\npnpm dev\n```\n\nYour MCP server is now running locally on port 3000.\n\n## Next Steps\n\nHead over to the [Configuration guide](./configuration.md) to customize your server.",
            updatedAt: "2026-04-01T09:30:00Z",
            author: "alice",
          },
          source: "github",
          feedback: {
            upvotes: 12,
            downvotes: 1,
            labels: ["beginner-friendly", "needs-update"],
            userVote: "up",
          },
          annotations: [
            {
              id: "ann-1",
              author: "claude-agent-7",
              authorType: "agent",
              content:
                "Users frequently ask about Docker setup here — consider adding a Docker section.",
              createdAt: "2026-03-30T14:00:00Z",
            },
          ],
        },
        {
          type: "file",
          name: "configuration.md",
          kind: "markdown",
          content:
            "# Configuration\n\nGram uses a layered configuration system.\n\n## Environment Variables\n\nSet `GRAM_API_KEY` in your environment or `.env` file.\n\n## gram.config.ts\n\nThe main configuration file supports:\n- `sources` — data source definitions\n- `tools` — tool registrations\n- `auth` — authentication providers",
          updatedAt: "2026-03-21T08:30:00Z",
          size: 278,
          versions: makeVersions(2, "2026-03-21T08:30:00Z", 278),
          source: "github",
          feedback: {
            upvotes: 8,
            downvotes: 0,
            labels: ["reference"],
            userVote: null,
          },
        },
        {
          type: "file",
          name: "SKILL.md",
          kind: "skill",
          content:
            "---\nname: getting-started\ndescription: Onboarding skill for new Gram users\n---\n\nWhen the user is new to Gram, walk them through creating their first project, connecting a data source, and testing in the playground.\n\nKey steps:\n1. Create a project via the dashboard\n2. Add an OpenAPI source or connect a catalog server\n3. Deploy and test in the playground",
          updatedAt: "2026-03-22T14:00:00Z",
          size: 340,
          versions: makeVersions(3, "2026-03-22T14:00:00Z", 340),
          source: "manual",
          feedback: {
            upvotes: 15,
            downvotes: 2,
            labels: ["onboarding"],
            userVote: "up",
          },
        },
      ],
    },
    {
      type: "folder",
      name: "guides",
      updatedAt: "2026-03-27T11:00:00Z",
      children: [
        {
          type: "file",
          name: "authentication.md",
          kind: "markdown",
          content:
            "# Authentication\n\nGram supports multiple authentication strategies.\n\n## OAuth 2.0\n\nConfigure OAuth providers in your MCP server settings.\n\n## API Keys\n\nGenerate API keys from the dashboard under Settings > API Keys.\n\n## Custom Auth\n\nImplement the `AuthProvider` interface for custom authentication flows.",
          updatedAt: "2026-03-26T09:15:00Z",
          size: 295,
          versions: makeVersions(5, "2026-03-26T09:15:00Z", 295),
          draft: {
            content:
              "# Authentication\n\nGram supports multiple authentication strategies.\n\n## OAuth 2.0\n\nConfigure OAuth providers in your MCP server settings.\n\n## API Keys\n\nGenerate API keys from the dashboard under Settings > API Keys.\n\n## JWT / Bearer Tokens\n\nPass a bearer token in the `Authorization` header. Gram validates it against your configured JWKS endpoint.\n\n## Custom Auth\n\nImplement the `AuthProvider` interface for custom authentication flows.",
            updatedAt: "2026-04-01T14:20:00Z",
            author: "bob",
          },
          source: "cli",
          feedback: {
            upvotes: 22,
            downvotes: 3,
            labels: ["security", "reference"],
            userVote: null,
          },
          annotations: [
            {
              id: "ann-2",
              author: "bob",
              authorType: "human",
              content: "JWT section is pending review from security team.",
              createdAt: "2026-04-01T14:25:00Z",
            },
            {
              id: "ann-3",
              author: "cursor-agent-12",
              authorType: "agent",
              content:
                "This page is the most-referenced in auth-related sessions. Consider splitting into sub-pages.",
              createdAt: "2026-03-31T10:00:00Z",
            },
          ],
        },
        {
          type: "file",
          name: "deployment.md",
          kind: "markdown",
          content:
            "# Deployment Guide\n\nDeploy your MCP servers to production.\n\n## Gram Cloud\n\nPush directly from the CLI:\n```bash\ngram deploy\n```\n\n## Self-Hosted\n\nUse the Docker image:\n```bash\ndocker pull gram/server:latest\n```\n\n## Custom Domains\n\nConfigure custom domains in the org settings.",
          updatedAt: "2026-03-27T11:00:00Z",
          size: 265,
          versions: makeVersions(3, "2026-03-27T11:00:00Z", 265),
          source: "cli",
          feedback: {
            upvotes: 6,
            downvotes: 0,
            labels: ["devops"],
            userVote: null,
          },
        },
        {
          type: "folder",
          name: "advanced",
          updatedAt: "2026-03-26T16:00:00Z",
          children: [
            {
              type: "file",
              name: ".docs-mcp.json",
              kind: "mcp-docs-config",
              config: {
                version: "1",
                strategy: { chunk_by: "h3", max_chunk_size: 8000 },
                metadata: { scope: "advanced-guide", audience: "power-users" },
              },
              updatedAt: "2026-03-24T10:00:00Z",
              size: 196,
              versions: makeVersions(2, "2026-03-24T10:00:00Z", 196),
              source: "manual",
            },
            {
              type: "file",
              name: "custom-tools.md",
              kind: "markdown",
              content:
                "# Building Custom Tools\n\nExtend your MCP server with custom tool definitions.\n\n## Tool Schema\n\nDefine tools using JSON Schema for inputs and outputs.\n\n## Validation\n\nGram automatically validates tool inputs against the schema.\n\n## Testing\n\nUse the playground to test tools interactively before deploying.",
              updatedAt: "2026-03-25T13:00:00Z",
              size: 310,
              versions: makeVersions(3, "2026-03-25T13:00:00Z", 310),
              source: "github",
              feedback: {
                upvotes: 9,
                downvotes: 1,
                labels: ["tools"],
                userVote: "up",
              },
            },
            {
              type: "file",
              name: "webhooks.md",
              kind: "markdown",
              content:
                "# Webhooks\n\nReceive real-time notifications for MCP events.\n\n## Setup\n\nConfigure webhook endpoints in Settings > Hooks.\n\n## Events\n\n- `tool.called` — fired when a tool is invoked\n- `session.started` — fired when a chat session begins\n- `deployment.completed` — fired after a successful deployment\n\n## Retry Policy\n\nFailed deliveries are retried with exponential backoff up to 3 times.",
              updatedAt: "2026-03-26T16:00:00Z",
              size: 380,
              versions: makeVersions(4, "2026-03-26T16:00:00Z", 380),
              source: "github",
            },
            {
              type: "file",
              name: "SKILL.md",
              kind: "skill",
              content:
                "---\nname: advanced-configuration\ndescription: Skill for advanced Gram configuration patterns\n---\n\nHelp experienced users with:\n- Multi-environment deployments\n- Custom authentication flows\n- Webhook integrations\n- Performance tuning for high-throughput MCP servers",
              updatedAt: "2026-03-26T09:00:00Z",
              size: 260,
              versions: makeVersions(2, "2026-03-26T09:00:00Z", 260),
              source: "manual",
              feedback: {
                upvotes: 5,
                downvotes: 0,
                labels: ["advanced"],
                userVote: null,
              },
            },
          ],
        },
      ],
    },
    {
      type: "folder",
      name: "api-reference",
      updatedAt: "2026-03-28T10:00:00Z",
      children: [
        {
          type: "file",
          name: "tools.md",
          kind: "markdown",
          content:
            "# Tools API\n\n## List Tools\n\n`GET /api/v1/tools`\n\nReturns all tools registered for the current project.\n\n## Get Tool\n\n`GET /api/v1/tools/:slug`\n\nReturns a single tool by slug.\n\n## Create Tool\n\n`POST /api/v1/tools`\n\nRegisters a new tool definition.",
          updatedAt: "2026-03-28T10:00:00Z",
          size: 242,
          versions: makeVersions(6, "2026-03-28T10:00:00Z", 242),
          draft: {
            content:
              "# Tools API\n\n## List Tools\n\n`GET /api/v1/tools`\n\nReturns all tools registered for the current project.\n\n### Query Parameters\n\n| Param | Type | Description |\n|-------|------|-------------|\n| `limit` | int | Max results (default 50) |\n| `offset` | int | Pagination offset |\n\n## Get Tool\n\n`GET /api/v1/tools/:slug`\n\nReturns a single tool by slug.\n\n## Create Tool\n\n`POST /api/v1/tools`\n\nRegisters a new tool definition.\n\n## Delete Tool\n\n`DELETE /api/v1/tools/:slug`\n\nRemoves a tool registration.",
            updatedAt: "2026-04-02T08:00:00Z",
            author: "carol",
          },
          source: "cli",
          feedback: {
            upvotes: 18,
            downvotes: 2,
            labels: ["api", "reference"],
            userVote: "up",
          },
        },
        {
          type: "file",
          name: "sources.md",
          kind: "markdown",
          content:
            "# Sources API\n\n## List Sources\n\n`GET /api/v1/sources`\n\nReturns all configured data sources.\n\n## Create Source\n\n`POST /api/v1/sources`\n\nAdds a new data source to the project.\n\n## Delete Source\n\n`DELETE /api/v1/sources/:id`\n\nRemoves a data source.",
          updatedAt: "2026-03-27T18:00:00Z",
          size: 230,
          versions: makeVersions(3, "2026-03-27T18:00:00Z", 230),
          source: "cli",
        },
        {
          type: "file",
          name: "sessions.md",
          kind: "markdown",
          content:
            "# Sessions API\n\n## List Sessions\n\n`GET /api/v1/sessions`\n\nReturns all chat sessions.\n\n## Get Session\n\n`GET /api/v1/sessions/:id`\n\nReturns a single session with its message history.\n\n## Create Session\n\n`POST /api/v1/sessions`\n\nStarts a new chat session.",
          updatedAt: "2026-03-26T14:00:00Z",
          size: 238,
          versions: makeVersions(2, "2026-03-26T14:00:00Z", 238),
          source: "cli",
        },
        {
          type: "file",
          name: "agent-notes.md",
          kind: "markdown",
          content:
            "# Common API Patterns\n\nThis document was generated by an agent based on recurring support patterns.\n\n## Pagination\n\nAll list endpoints support `limit` and `offset` query parameters.\n\n## Error Handling\n\nAll endpoints return standard error objects with `code` and `message` fields.",
          updatedAt: "2026-04-01T16:00:00Z",
          size: 220,
          versions: makeVersions(1, "2026-04-01T16:00:00Z", 220),
          source: "agent",
          feedback: {
            upvotes: 3,
            downvotes: 0,
            labels: ["agent-generated"],
            userVote: null,
          },
          annotations: [
            {
              id: "ann-4",
              author: "claude-agent-7",
              authorType: "agent",
              content:
                "Created from patterns observed across 47 support sessions.",
              createdAt: "2026-04-01T16:00:00Z",
            },
          ],
        },
      ],
    },
    {
      type: "folder",
      name: "sdk",
      updatedAt: "2026-03-24T17:00:00Z",
      children: [
        {
          type: "file",
          name: ".docs-mcp.json",
          kind: "mcp-docs-config",
          config: {
            version: "1",
            metadata: { scope: "sdk-specific" },
            taxonomy: {
              language: {
                vector_collapse: true,
                properties: {
                  typescript: { mcp_resource: true },
                  python: { mcp_resource: true },
                },
              },
            },
          },
          updatedAt: "2026-03-23T10:00:00Z",
          size: 320,
          versions: makeVersions(1, "2026-03-23T10:00:00Z", 320),
          source: "manual",
        },
        {
          type: "folder",
          name: "typescript",
          updatedAt: "2026-03-24T17:00:00Z",
          children: [
            {
              type: "file",
              name: "quickstart.md",
              kind: "markdown",
              content:
                "# TypeScript SDK Quickstart\n\n```bash\nnpm install @gram/sdk\n```\n\n## Initialize\n\n```typescript\nimport { Gram } from '@gram/sdk';\n\nconst gram = new Gram({ apiKey: process.env.GRAM_API_KEY });\n```\n\n## Call a Tool\n\n```typescript\nconst result = await gram.tools.call('my-tool', { input: 'hello' });\n```",
              updatedAt: "2026-03-24T17:00:00Z",
              size: 290,
              versions: makeVersions(3, "2026-03-24T17:00:00Z", 290),
              source: "github",
              feedback: {
                upvotes: 14,
                downvotes: 0,
                labels: ["typescript", "quickstart"],
                userVote: "up",
              },
            },
            {
              type: "file",
              name: "SKILL.md",
              kind: "skill",
              content:
                "---\nname: typescript-sdk\ndescription: Skill for TypeScript SDK usage patterns\n---\n\nGuide users through TypeScript SDK patterns:\n- Client initialization and configuration\n- Tool invocation and error handling\n- Streaming responses\n- Type-safe tool definitions with Zod schemas",
              updatedAt: "2026-03-23T16:00:00Z",
              size: 250,
              versions: makeVersions(2, "2026-03-23T16:00:00Z", 250),
              source: "manual",
            },
          ],
        },
        {
          type: "folder",
          name: "python",
          updatedAt: "2026-03-24T15:00:00Z",
          children: [
            {
              type: "file",
              name: "quickstart.md",
              kind: "markdown",
              content:
                "# Python SDK Quickstart\n\n```bash\npip install gram-sdk\n```\n\n## Initialize\n\n```python\nfrom gram import Gram\n\ngram = Gram(api_key=os.environ['GRAM_API_KEY'])\n```\n\n## Call a Tool\n\n```python\nresult = gram.tools.call('my-tool', input='hello')\n```",
              updatedAt: "2026-03-24T15:00:00Z",
              size: 245,
              versions: makeVersions(4, "2026-03-24T15:00:00Z", 245),
              source: "github",
            },
          ],
        },
      ],
    },
  ],
};

// ── Mock skills registry ──────────────────────────────────────────────────

export const MOCK_REGISTRY_SKILLS: RegistrySkill[] = [
  {
    id: "skill-getting-started",
    name: "getting-started",
    description: "Onboarding skill for new Gram users",
    body: "When the user is new to Gram, walk them through creating their first project, connecting a data source, and testing in the playground.\n\nKey steps:\n1. Create a project via the dashboard\n2. Add an OpenAPI source or connect a catalog server\n3. Deploy and test in the playground",
    source: "corpus",
    status: "active",
    author: "alice",
    path: "getting-started/SKILL.md",
    updatedAt: "2026-03-22T14:00:00Z",
    invocations: 847,
    frontmatter: {
      name: "getting-started",
      description: "Onboarding skill for new Gram users",
    },
  },
  {
    id: "skill-advanced-config",
    name: "advanced-configuration",
    description: "Skill for advanced Gram configuration patterns",
    body: "Help experienced users with:\n- Multi-environment deployments\n- Custom authentication flows\n- Webhook integrations\n- Performance tuning for high-throughput MCP servers",
    source: "corpus",
    status: "active",
    author: "carol",
    path: "guides/advanced/SKILL.md",
    updatedAt: "2026-03-26T09:00:00Z",
    invocations: 234,
    frontmatter: {
      name: "advanced-configuration",
      description: "Skill for advanced Gram configuration patterns",
    },
  },
  {
    id: "skill-ts-sdk",
    name: "typescript-sdk",
    description: "Skill for TypeScript SDK usage patterns",
    body: "Guide users through TypeScript SDK patterns:\n- Client initialization and configuration\n- Tool invocation and error handling\n- Streaming responses\n- Type-safe tool definitions with Zod schemas",
    source: "corpus",
    status: "active",
    author: "alice",
    path: "sdk/typescript/SKILL.md",
    updatedAt: "2026-03-23T16:00:00Z",
    invocations: 562,
    frontmatter: {
      name: "typescript-sdk",
      description: "Skill for TypeScript SDK usage patterns",
    },
  },
  {
    id: "skill-debug-mcp",
    name: "debug-mcp-connections",
    description: "Troubleshoot MCP server connectivity issues",
    body: "When an agent cannot connect to an MCP server:\n1. Verify the server URL in environment settings\n2. Check authentication credentials\n3. Inspect the MCP logs for handshake errors\n4. Test with the playground using verbose mode\n5. Verify network/firewall rules",
    source: "captured",
    capturedFrom: {
      sessionId: "sess-abc123",
      agentName: "claude-agent-7",
      capturedAt: "2026-03-29T11:00:00Z",
    },
    status: "active",
    author: "claude-agent-7",
    updatedAt: "2026-03-29T11:00:00Z",
    invocations: 89,
    frontmatter: {
      name: "debug-mcp-connections",
      description: "Troubleshoot MCP server connectivity issues",
    },
  },
  {
    id: "skill-migrate-v2",
    name: "migrate-to-v2",
    description: "Guide migration from Gram v1 to v2 API",
    body: "Walk users through the v1 to v2 migration:\n- Updated authentication flow\n- New tool registration format\n- Deprecated endpoints and replacements\n- Data migration scripts",
    source: "captured",
    capturedFrom: {
      sessionId: "sess-def456",
      agentName: "cursor-agent-12",
      capturedAt: "2026-03-31T09:00:00Z",
    },
    status: "pending-review",
    author: "cursor-agent-12",
    updatedAt: "2026-03-31T09:00:00Z",
    invocations: 12,
    frontmatter: {
      name: "migrate-to-v2",
      description: "Guide migration from Gram v1 to v2 API",
    },
  },
  {
    id: "skill-custom-uploaded",
    name: "internal-deployment-checklist",
    description: "Internal pre-deployment verification checklist",
    body: "Before deploying to production:\n- [ ] Run full test suite\n- [ ] Verify environment variables\n- [ ] Check rate limit configuration\n- [ ] Validate OAuth redirect URIs\n- [ ] Review access control rules\n- [ ] Confirm monitoring alerts are configured",
    source: "uploaded",
    status: "active",
    author: "dave",
    updatedAt: "2026-04-01T10:00:00Z",
    invocations: 156,
    frontmatter: {
      name: "internal-deployment-checklist",
      description: "Internal pre-deployment verification checklist",
    },
  },
];

// ── Mock observability data ───────────────────────────────────────────────

export const MOCK_SEARCH_LOGS: SearchLogEntry[] = [
  {
    id: "s1",
    query: "how to authenticate with OAuth",
    filters: { language: "typescript" },
    resultsCount: 5,
    topChunkPath: "guides/authentication.md#oauth-2-0",
    latencyMs: 42,
    sessionId: "sess-001",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:30:00Z",
  },
  {
    id: "s2",
    query: "deploy MCP server",
    resultsCount: 3,
    topChunkPath: "guides/deployment.md#gram-cloud",
    latencyMs: 38,
    sessionId: "sess-002",
    agentName: "cursor-agent-12",
    timestamp: "2026-04-02T15:28:00Z",
  },
  {
    id: "s3",
    query: "create custom tool schema",
    filters: { scope: "advanced-guide" },
    resultsCount: 2,
    topChunkPath: "guides/advanced/custom-tools.md#tool-schema",
    latencyMs: 55,
    sessionId: "sess-003",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:25:00Z",
  },
  {
    id: "s4",
    query: "TypeScript SDK initialization",
    filters: { language: "typescript" },
    resultsCount: 4,
    topChunkPath: "sdk/typescript/quickstart.md#initialize",
    latencyMs: 31,
    sessionId: "sess-004",
    agentName: "windsurf-agent-3",
    timestamp: "2026-04-02T15:20:00Z",
  },
  {
    id: "s5",
    query: "webhook retry policy",
    resultsCount: 1,
    topChunkPath: "guides/advanced/webhooks.md#retry-policy",
    latencyMs: 28,
    sessionId: "sess-001",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:15:00Z",
  },
  {
    id: "s6",
    query: "list all tools API",
    filters: { scope: "public-docs" },
    resultsCount: 2,
    topChunkPath: "api-reference/tools.md#list-tools",
    latencyMs: 35,
    sessionId: "sess-005",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:10:00Z",
  },
  {
    id: "s7",
    query: "Python SDK quickstart",
    filters: { language: "python" },
    resultsCount: 3,
    topChunkPath: "sdk/python/quickstart.md#initialize",
    latencyMs: 44,
    sessionId: "sess-006",
    agentName: "cursor-agent-12",
    timestamp: "2026-04-02T15:05:00Z",
  },
  {
    id: "s8",
    query: "gram configuration file",
    resultsCount: 4,
    topChunkPath: "getting-started/configuration.md#gram-config-ts",
    latencyMs: 39,
    sessionId: "sess-007",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:00:00Z",
  },
];

export const MOCK_SKILL_INVOCATIONS: SkillInvocationEntry[] = [
  {
    id: "i1",
    skillId: "skill-getting-started",
    skillName: "getting-started",
    sessionId: "sess-001",
    agentName: "claude-agent-7",
    latencyMs: 12,
    timestamp: "2026-04-02T15:32:00Z",
    success: true,
  },
  {
    id: "i2",
    skillId: "skill-ts-sdk",
    skillName: "typescript-sdk",
    sessionId: "sess-004",
    agentName: "windsurf-agent-3",
    latencyMs: 8,
    timestamp: "2026-04-02T15:22:00Z",
    success: true,
  },
  {
    id: "i3",
    skillId: "skill-debug-mcp",
    skillName: "debug-mcp-connections",
    sessionId: "sess-003",
    agentName: "claude-agent-7",
    latencyMs: 15,
    timestamp: "2026-04-02T15:18:00Z",
    success: true,
  },
  {
    id: "i4",
    skillId: "skill-advanced-config",
    skillName: "advanced-configuration",
    sessionId: "sess-002",
    agentName: "cursor-agent-12",
    latencyMs: 10,
    timestamp: "2026-04-02T15:12:00Z",
    success: true,
  },
  {
    id: "i5",
    skillId: "skill-custom-uploaded",
    skillName: "internal-deployment-checklist",
    sessionId: "sess-005",
    agentName: "claude-agent-7",
    latencyMs: 9,
    timestamp: "2026-04-02T15:08:00Z",
    success: true,
  },
  {
    id: "i6",
    skillId: "skill-migrate-v2",
    skillName: "migrate-to-v2",
    sessionId: "sess-006",
    agentName: "cursor-agent-12",
    latencyMs: 250,
    timestamp: "2026-04-02T14:55:00Z",
    success: false,
  },
];

// ── Helpers ────────────────────────────────────────────────────────────────

export function resolvePath(
  root: ContextFolder,
  segments: string[],
): ContextFolder | null {
  let current: ContextFolder = root;
  for (const seg of segments) {
    const child = current.children.find(
      (c) => c.type === "folder" && c.name === seg,
    );
    if (!child || child.type !== "folder") return null;
    current = child;
  }
  return current;
}

export function findFile(
  folder: ContextFolder,
  name: string,
): ContextFile | null {
  const node = folder.children.find(
    (c) => c.type === "file" && c.name === name,
  );
  return node?.type === "file" ? node : null;
}

export function countItems(folder: ContextFolder): {
  folders: number;
  files: number;
} {
  let folders = 0;
  let files = 0;
  for (const child of folder.children) {
    if (child.type === "folder") {
      folders++;
      const sub = countItems(child);
      folders += sub.folders;
      files += sub.files;
    } else {
      files++;
    }
  }
  return { folders, files };
}

export function getEffectiveConfig(
  root: ContextFolder,
  segments: string[],
): DocsMcpConfig | null {
  let config: DocsMcpConfig | null = null;
  const rootConfig = root.children.find(
    (c) => c.type === "file" && c.kind === "mcp-docs-config",
  );
  if (rootConfig?.type === "file" && rootConfig.config)
    config = rootConfig.config;

  let current: ContextFolder = root;
  for (const seg of segments) {
    const child = current.children.find(
      (c) => c.type === "folder" && c.name === seg,
    );
    if (!child || child.type !== "folder") break;
    current = child;
    const localConfig = current.children.find(
      (c) => c.type === "file" && c.kind === "mcp-docs-config",
    );
    if (localConfig?.type === "file" && localConfig.config)
      config = { ...config, ...localConfig.config };
  }
  return config;
}

export function hasDraft(node: ContextNode): boolean {
  if (node.type === "file") return !!node.draft;
  return node.children.some(hasDraft);
}

export function countDrafts(folder: ContextFolder): number {
  let count = 0;
  for (const child of folder.children) {
    if (child.type === "file" && child.draft) count++;
    if (child.type === "folder") count += countDrafts(child);
  }
  return count;
}

export function collectDrafts(
  folder: ContextFolder,
  parentPath: string[] = [],
): Array<{ file: ContextFile; path: string[] }> {
  const results: Array<{ file: ContextFile; path: string[] }> = [];
  for (const child of folder.children) {
    if (child.type === "file" && child.draft)
      results.push({ file: child, path: parentPath });
    if (child.type === "folder")
      results.push(...collectDrafts(child, [...parentPath, child.name]));
  }
  return results;
}

// ── Simple line-based diff ────────────────────────────────────────────────

export type DiffLine = {
  type: "same" | "added" | "removed";
  content: string;
  lineNumber?: number;
};

export function computeLineDiff(oldText: string, newText: string): DiffLine[] {
  const oldLines = oldText.split("\n");
  const newLines = newText.split("\n");
  const result: DiffLine[] = [];
  const m = oldLines.length;
  const n = newLines.length;
  const dp: number[][] = Array.from({ length: m + 1 }, () =>
    Array(n + 1).fill(0),
  );
  for (let i = 1; i <= m; i++) {
    for (let j = 1; j <= n; j++) {
      dp[i][j] =
        oldLines[i - 1] === newLines[j - 1]
          ? dp[i - 1][j - 1] + 1
          : Math.max(dp[i - 1][j], dp[i][j - 1]);
    }
  }
  let i = m;
  let j = n;
  const stack: DiffLine[] = [];
  while (i > 0 || j > 0) {
    if (i > 0 && j > 0 && oldLines[i - 1] === newLines[j - 1]) {
      stack.push({ type: "same", content: oldLines[i - 1], lineNumber: j });
      i--;
      j--;
    } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
      stack.push({ type: "added", content: newLines[j - 1], lineNumber: j });
      j--;
    } else {
      stack.push({ type: "removed", content: oldLines[i - 1] });
      i--;
    }
  }
  for (let k = stack.length - 1; k >= 0; k--) result.push(stack[k]);
  return result;
}

export function collectSkills(
  folder: ContextFolder,
  parentPath: string[] = [],
): Array<{ file: ContextFile; path: string[] }> {
  const results: Array<{ file: ContextFile; path: string[] }> = [];
  for (const child of folder.children) {
    if (child.type === "file" && child.kind === "skill")
      results.push({ file: child, path: parentPath });
    if (child.type === "folder")
      results.push(...collectSkills(child, [...parentPath, child.name]));
  }
  return results;
}

export function parseSkillFrontmatter(content: string): {
  meta: Record<string, string>;
  body: string;
} {
  const match = content.match(/^---\n([\s\S]*?)\n---\n?([\s\S]*)$/);
  if (!match) return { meta: {}, body: content };
  const meta: Record<string, string> = {};
  for (const line of match[1].split("\n")) {
    const colonIdx = line.indexOf(":");
    if (colonIdx > 0) {
      meta[line.slice(0, colonIdx).trim()] = line.slice(colonIdx + 1).trim();
    }
  }
  return { meta, body: match[2].trim() };
}

// ── Draft Documents (pending changes with social features) ────────────────

export type DraftComment = {
  id: string;
  author: string;
  authorType: "human" | "agent";
  content: string;
  createdAt: string;
  upvotes: number;
};

export type DraftDocument = {
  id: string;
  title: string;
  author: string;
  authorType: "human" | "agent";
  createdAt: string;
  updatedAt: string;
  /** Existing file path if this is an edit, null if new doc */
  filePath: string | null;
  /** For new docs: proposed location in the corpus */
  proposedPath?: string;
  /** For edits: the original content */
  originalContent?: string;
  /** The proposed content */
  content: string;
  upvotes: number;
  downvotes: number;
  userVote?: "up" | "down" | null;
  comments: DraftComment[];
  status: "open" | "published" | "rejected";
  labels: string[];
};

export const MOCK_DRAFT_DOCUMENTS: DraftDocument[] = [
  {
    id: "draft-1",
    title: "Update prerequisites to Node 20+ and pnpm",
    author: "alice",
    authorType: "human",
    createdAt: "2026-04-01T09:30:00Z",
    updatedAt: "2026-04-01T15:00:00Z",
    filePath: "getting-started/introduction.md",
    originalContent:
      "# Introduction\n\nWelcome to Gram! This guide walks you through the basics of setting up your first MCP server.\n\n## Prerequisites\n\n- Node.js 18+\n- A Gram account\n\n## Quick Start\n\n```bash\nnpx create-gram-app my-server\ncd my-server\nnpm run dev\n```\n\nYour MCP server is now running locally on port 3000.",
    content:
      "# Introduction\n\nWelcome to Gram! This guide walks you through the basics of setting up your first MCP server.\n\n## Prerequisites\n\n- Node.js 20+\n- A Gram account\n- pnpm 9+\n\n## Quick Start\n\n```bash\nnpx create-gram-app my-server\ncd my-server\npnpm dev\n```\n\nYour MCP server is now running locally on port 3000.\n\n## Next Steps\n\nHead over to the [Configuration guide](./configuration.md) to customize your server.",
    upvotes: 7,
    downvotes: 1,
    userVote: "up",
    status: "open",
    labels: ["documentation", "breaking-change"],
    comments: [
      {
        id: "c1",
        author: "bob",
        authorType: "human",
        content: "Good call on pnpm — we should update the CI templates too.",
        createdAt: "2026-04-01T10:15:00Z",
        upvotes: 3,
      },
      {
        id: "c2",
        author: "claude-agent-7",
        authorType: "agent",
        content:
          "I've seen 12 support sessions this week where users had issues with npm. pnpm resolves the peer dependency conflicts.",
        createdAt: "2026-04-01T11:00:00Z",
        upvotes: 5,
      },
    ],
  },
  {
    id: "draft-2",
    title: "Add JWT / Bearer Token authentication section",
    author: "bob",
    authorType: "human",
    createdAt: "2026-04-01T14:20:00Z",
    updatedAt: "2026-04-01T14:20:00Z",
    filePath: "guides/authentication.md",
    originalContent:
      "# Authentication\n\nGram supports multiple authentication strategies.\n\n## OAuth 2.0\n\nConfigure OAuth providers in your MCP server settings.\n\n## API Keys\n\nGenerate API keys from the dashboard under Settings > API Keys.\n\n## Custom Auth\n\nImplement the `AuthProvider` interface for custom authentication flows.",
    content:
      "# Authentication\n\nGram supports multiple authentication strategies.\n\n## OAuth 2.0\n\nConfigure OAuth providers in your MCP server settings.\n\n## API Keys\n\nGenerate API keys from the dashboard under Settings > API Keys.\n\n## JWT / Bearer Tokens\n\nPass a bearer token in the `Authorization` header. Gram validates it against your configured JWKS endpoint.\n\n## Custom Auth\n\nImplement the `AuthProvider` interface for custom authentication flows.",
    upvotes: 14,
    downvotes: 0,
    userVote: null,
    status: "open",
    labels: ["security", "enhancement"],
    comments: [
      {
        id: "c3",
        author: "carol",
        authorType: "human",
        content: "Security team approved this. Ship it!",
        createdAt: "2026-04-01T16:00:00Z",
        upvotes: 8,
      },
    ],
  },
  {
    id: "draft-3",
    title: "Add query parameters and Delete endpoint to Tools API",
    author: "carol",
    authorType: "human",
    createdAt: "2026-04-02T08:00:00Z",
    updatedAt: "2026-04-02T08:00:00Z",
    filePath: "api-reference/tools.md",
    originalContent:
      "# Tools API\n\n## List Tools\n\n`GET /api/v1/tools`\n\nReturns all tools registered for the current project.\n\n## Get Tool\n\n`GET /api/v1/tools/:slug`\n\nReturns a single tool by slug.\n\n## Create Tool\n\n`POST /api/v1/tools`\n\nRegisters a new tool definition.",
    content:
      "# Tools API\n\n## List Tools\n\n`GET /api/v1/tools`\n\nReturns all tools registered for the current project.\n\n### Query Parameters\n\n| Param | Type | Description |\n|-------|------|-------------|\n| `limit` | int | Max results (default 50) |\n| `offset` | int | Pagination offset |\n\n## Get Tool\n\n`GET /api/v1/tools/:slug`\n\nReturns a single tool by slug.\n\n## Create Tool\n\n`POST /api/v1/tools`\n\nRegisters a new tool definition.\n\n## Delete Tool\n\n`DELETE /api/v1/tools/:slug`\n\nRemoves a tool registration.",
    upvotes: 4,
    downvotes: 2,
    userVote: "up",
    status: "open",
    labels: ["api"],
    comments: [
      {
        id: "c4",
        author: "dave",
        authorType: "human",
        content: "Should we add a soft-delete option instead?",
        createdAt: "2026-04-02T09:00:00Z",
        upvotes: 2,
      },
      {
        id: "c5",
        author: "claude-agent-7",
        authorType: "agent",
        content:
          "Based on user feedback, hard delete is the expected behavior. Soft delete can be a follow-up.",
        createdAt: "2026-04-02T09:30:00Z",
        upvotes: 1,
      },
      {
        id: "c6",
        author: "carol",
        authorType: "human",
        content: "Agreed with the agent. Let's keep it simple for v1.",
        createdAt: "2026-04-02T10:00:00Z",
        upvotes: 3,
      },
    ],
  },
  {
    id: "draft-4",
    title: "New doc: Rate Limiting Best Practices",
    author: "cursor-agent-12",
    authorType: "agent",
    createdAt: "2026-04-02T11:00:00Z",
    updatedAt: "2026-04-02T11:00:00Z",
    filePath: null,
    proposedPath: "guides/advanced/rate-limiting.md",
    content:
      "# Rate Limiting Best Practices\n\nThis guide covers rate limiting strategies for MCP servers on Gram.\n\n## Default Limits\n\n- 100 requests/minute per API key\n- 1000 tool calls/hour per session\n\n## Custom Limits\n\nConfigure per-toolset limits in the environment settings.\n\n## Handling 429 Responses\n\nWhen rate limited, clients receive a `429` status with a `Retry-After` header.\n\n## Monitoring\n\nTrack rate limit usage in the Observability dashboard.",
    upvotes: 11,
    downvotes: 1,
    userVote: null,
    status: "open",
    labels: ["agent-generated", "new-doc", "best-practices"],
    comments: [
      {
        id: "c7",
        author: "alice",
        authorType: "human",
        content:
          "Great initiative from the agent. The numbers look right. Should we add a section on burst limits?",
        createdAt: "2026-04-02T12:00:00Z",
        upvotes: 4,
      },
    ],
  },
  {
    id: "draft-5",
    title: "New doc: Troubleshooting Common MCP Connection Issues",
    author: "claude-agent-7",
    authorType: "agent",
    createdAt: "2026-04-02T13:00:00Z",
    updatedAt: "2026-04-02T13:00:00Z",
    filePath: null,
    proposedPath: "guides/troubleshooting.md",
    content:
      "# Troubleshooting Common MCP Connection Issues\n\nBased on patterns observed across 89 support sessions.\n\n## Connection Refused\n\nCheck that the server URL is correct and the server is running.\n\n## Authentication Failures\n\nVerify API keys haven't expired. Check OAuth token refresh.\n\n## Timeout Errors\n\nIncrease the client timeout setting. Default is 30s.\n\n## SSL Certificate Errors\n\nEnsure your custom domain has a valid certificate.",
    upvotes: 19,
    downvotes: 0,
    userVote: "up",
    status: "open",
    labels: ["agent-generated", "new-doc", "troubleshooting"],
    comments: [
      {
        id: "c8",
        author: "bob",
        authorType: "human",
        content:
          "This would've saved us so many support tickets. Publish ASAP.",
        createdAt: "2026-04-02T13:30:00Z",
        upvotes: 12,
      },
      {
        id: "c9",
        author: "dave",
        authorType: "human",
        content: "Can we add a section on firewall/proxy issues too?",
        createdAt: "2026-04-02T14:00:00Z",
        upvotes: 6,
      },
    ],
  },
  {
    id: "draft-6",
    title: "Add external-partner role to access control config",
    author: "dave",
    authorType: "human",
    createdAt: "2026-04-02T14:30:00Z",
    updatedAt: "2026-04-02T14:30:00Z",
    filePath: ".docs-mcp.json",
    originalContent: JSON.stringify(
      {
        version: "1",
        strategy: {
          chunk_by: "h2",
          max_chunk_size: 12000,
          min_chunk_size: 200,
        },
        accessControl: [
          {
            role: "developer",
            allowedTaxonomy: { language: ["typescript", "python", "go"] },
          },
          {
            role: "support",
            allowedTaxonomy: { language: ["typescript", "python"] },
            deniedPaths: ["guides/advanced/*"],
          },
        ],
      },
      null,
      2,
    ),
    content: JSON.stringify(
      {
        version: "1",
        strategy: {
          chunk_by: "h2",
          max_chunk_size: 12000,
          min_chunk_size: 200,
        },
        accessControl: [
          {
            role: "developer",
            allowedTaxonomy: { language: ["typescript", "python", "go"] },
          },
          {
            role: "support",
            allowedTaxonomy: { language: ["typescript", "python"] },
            deniedPaths: ["guides/advanced/*"],
          },
          {
            role: "external-partner",
            allowedTaxonomy: { language: ["typescript"] },
            deniedPaths: ["api-reference/*", "guides/advanced/*"],
          },
        ],
      },
      null,
      2,
    ),
    upvotes: 6,
    downvotes: 0,
    userVote: "up",
    status: "open",
    labels: ["config", "access-control"],
    comments: [
      {
        id: "c10",
        author: "alice",
        authorType: "human",
        content:
          "Makes sense — partners shouldn't see our internal API docs or advanced guides.",
        createdAt: "2026-04-02T15:00:00Z",
        upvotes: 4,
      },
    ],
  },
];

export function formatRelativeTime(iso: string): string {
  const now = new Date("2026-04-02T16:00:00Z").getTime();
  const then = new Date(iso).getTime();
  const diffMs = now - then;
  const diffMins = Math.floor(diffMs / 60000);
  if (diffMins < 60) return `${diffMins}m ago`;
  const diffHours = Math.floor(diffMins / 60);
  if (diffHours < 24) return `${diffHours}h ago`;
  const diffDays = Math.floor(diffHours / 24);
  return `${diffDays}d ago`;
}

export function formatFileSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  return `${(bytes / 1024).toFixed(1)} KB`;
}

export function formatDate(iso: string): string {
  return new Date(iso).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

export function formatTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("en-US", {
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

export function sourceLabel(source?: string): string {
  switch (source) {
    case "github":
      return "GitHub";
    case "cli":
      return "CLI";
    case "agent":
      return "Agent";
    case "manual":
      return "Manual";
    default:
      return "Unknown";
  }
}
