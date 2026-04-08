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
  /** User-editable attribution (who authored the change). */
  author: string;
  /** System-set, immutable (who actually committed it). */
  committer: string;
  /** AI agent that produced the change, if any. */
  agent?: "claude-code" | "codex" | "cursor" | "copilot";
  message: string;
  size: number;
  content?: string;
  /** Path at this version — changes indicate a move/rename. */
  path?: string;
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

export type FeedbackComment = {
  id: string;
  author: string;
  authorType: "human" | "agent";
  content: string;
  createdAt: string;
  upvotes: number;
  downvotes: number;
};

export type DocFeedback = {
  upvotes: number;
  downvotes: number;
  labels: string[];
  userVote?: "up" | "down" | null;
  comments: FeedbackComment[];
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

export type SkillVisibility =
  | { mode: "all" }
  | { mode: "allow"; roles: string[] }
  | { mode: "deny"; roles: string[] }
  | { mode: "none" };

export type SkillOriginChannel =
  | "bundled"
  | "managed"
  | "user"
  | "project"
  | "plugin"
  | "mcp";

export type SkillDistributionMechanism =
  | "skills_dir"
  | "legacy_commands_dir"
  | "plugin_package"
  | "mcp_server"
  | "bundled_binary";

export type SkillTrustTier = "high" | "medium" | "low" | "untrusted";

export type SkillProvenance = {
  originChannel: SkillOriginChannel;
  distributionMechanism: SkillDistributionMechanism;
  trustTier: SkillTrustTier;
  pluginName?: string;
  mcpServerName?: string;
};

// ── Security audit types ──────────────────────────────────────────────────

export type AuditRiskLevel = "safe" | "caution" | "warning" | "critical";

export type AuditCheck = {
  category: "malicious" | "security" | "obfuscation" | "suspicious";
  label: string;
  status: "pass" | "info" | "warn" | "fail";
  detail: string;
};

export type SkillAudit = {
  riskLevel: AuditRiskLevel;
  analyzedAt: string;
  contentHash: string;
  checks: AuditCheck[];
  /** Per-skill full analysis — a narrative judgement of the skill's behavior. */
  analysis: string;
};

/** Immutable content snapshot — identified by content hash. */
export type SkillDigest = {
  contentHash: string;
  pushedAt: string;
  pushedBy: string;
  bodyBytes: number;
  provenance: SkillProvenance;
  message?: string;
  audit?: SkillAudit;
};

/** A mutable tag pointing at a digest. "latest" is always present. */
export type SkillTag = {
  tag: string;
  contentHash: string;
  updatedAt: string;
  updatedBy: string;
};

export type SkillInsights = {
  installations: number;
  activeInstallations: number;
  /** Percentage of installations on the latest version (0-100). */
  pctOnLatest: number;
  avgTokens: number;
  invocations7d: number;
  successRate: number;
};

export type RegistrySkill = {
  id: string;
  name: string;
  description: string;
  body: string;
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
  frontmatter: Record<string, string>;
  /** Which roles can see / invoke this skill. Defaults to "all". */
  visibility?: SkillVisibility;
  /** Immutable content snapshots — newest first. */
  digests: SkillDigest[];
  /** Mutable tags pointing at digests. "latest" is always present. */
  tags: SkillTag[];
  insights: SkillInsights;
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

/** All known roles from the access control config. */
export const MOCK_ALL_ROLES = ["engineering", "sales", "finance"] as const;

export const MOCK_CAPTURE_SETTINGS: CaptureSettings = {
  enabled: true,
  captureProjectSkills: true,
  captureUserSkills: false,
  ignoreWithFrontmatter: true,
};

// ── Version history helper ────────────────────────────────────────────────

type MakeVersionsOpts = {
  content?: string;
  /** Current path of the file — used to simulate renames in older versions. */
  currentPath?: string;
};

function makeVersions(
  count: number,
  latestDate: string,
  latestSize: number,
  opts?: MakeVersionsOpts,
): FileVersion[] {
  const authors = ["alice", "bob", "carol", "dave"];
  const committers = ["alice", "bob", "system", "deploy-bot"];
  const agents: (FileVersion["agent"] | undefined)[] = [
    undefined,
    "claude-code",
    undefined,
    "codex",
    "cursor",
    undefined,
    "copilot",
  ];
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
  const contentParagraphs = opts?.content?.split("\n\n") ?? [];

  for (let i = count; i >= 1; i--) {
    const idx = count - i;
    const daysBack = idx * 3;
    // Build progressively shorter content for older versions
    const paraCount = contentParagraphs.length;
    const parasForVersion =
      paraCount > 0 ? Math.max(1, Math.round(paraCount * (i / count))) : 0;
    const versionContent =
      parasForVersion > 0
        ? contentParagraphs.slice(0, parasForVersion).join("\n\n")
        : undefined;

    // Simulate a rename in oldest version if there are enough versions
    let path = opts?.currentPath;
    if (i === 1 && count > 2 && opts?.currentPath) {
      const parts = opts.currentPath.split("/");
      const filename = parts.pop()!;
      parts.push(`draft-${filename}`);
      path = parts.join("/");
    }

    const agent = agents[idx % agents.length];
    versions.push({
      version: i,
      updatedAt: new Date(base - daysBack * 86400000).toISOString(),
      author: authors[idx % authors.length],
      committer: committers[idx % committers.length],
      ...(agent && { agent }),
      message: messages[idx % messages.length],
      size: Math.max(100, latestSize - idx * 30),
      ...(versionContent && { content: versionContent }),
      ...(path && path !== opts?.currentPath && { path }),
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
        metadata: { product: "acme-saas", scope: "company-docs" },
        taxonomy: {
          department: {
            vector_collapse: true,
            properties: {
              engineering: { mcp_resource: true },
              sales: { mcp_resource: true },
              finance: { mcp_resource: false },
            },
          },
          audience: {
            vector_collapse: false,
            properties: {
              internal: { mcp_resource: true },
              external: { mcp_resource: true },
              partner: { mcp_resource: false },
            },
          },
        },
        mcpServerInstructions:
          "This server provides Acme Corp internal documentation. Use the search tool to find relevant company docs, engineering guides, and department-specific resources.",
        overrides: [
          {
            pattern: "engineering/runbooks/*.md",
            strategy: { chunk_by: "h3" },
            metadata: { scope: "incident-response" },
          },
        ],
        accessControl: [
          {
            role: "engineering",
            allowedTaxonomy: {
              department: ["engineering"],
              audience: ["internal", "external", "partner"],
            },
          },
          {
            role: "sales",
            allowedTaxonomy: {
              department: ["sales"],
              audience: ["internal", "external", "partner"],
            },
            deniedPaths: ["engineering/*", "finance/*"],
          },
          {
            role: "finance",
            allowedTaxonomy: {
              department: ["finance"],
              audience: ["internal"],
            },
            deniedPaths: ["engineering/*", "sales/*"],
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
      name: "product",
      updatedAt: "2026-03-25T09:00:00Z",
      children: [
        {
          type: "file",
          name: "overview.md",
          kind: "markdown",
          content:
            "# Product Overview\n\nAcme SaaS is a B2B platform that helps companies manage their data infrastructure at scale.\n\n## Core Features\n\n- Automated data pipelines\n- Real-time analytics dashboard\n- Role-based access control\n- API-first architecture\n\n## Target Market\n\nMid-market and enterprise companies processing 1M+ events per day.",
          updatedAt: "2026-03-20T12:00:00Z",
          size: 345,
          versions: makeVersions(4, "2026-03-20T12:00:00Z", 345),
          draft: {
            content:
              "# Product Overview\n\nAcme SaaS is a B2B platform that helps companies manage their data infrastructure at scale.\n\n## Core Features\n\n- Automated data pipelines\n- Real-time analytics dashboard\n- Role-based access control\n- API-first architecture\n- AI-powered anomaly detection (NEW)\n\n## Target Market\n\nMid-market and enterprise companies processing 1M+ events per day.\n\n## Recent Updates\n\nSee the [roadmap](./roadmap.md) for upcoming features and timelines.",
            updatedAt: "2026-04-01T09:30:00Z",
            author: "alice",
          },
          source: "github",
          feedback: {
            upvotes: 12,
            downvotes: 1,
            labels: ["product", "needs-update"],
            userVote: "up",
            comments: [
              {
                id: "fc1",
                author: "bob",
                authorType: "human",
                content:
                  "This page is the first thing new hires read. Great overview.",
                createdAt: "2026-03-22T10:00:00Z",
                upvotes: 8,
                downvotes: 0,
              },
              {
                id: "fc2",
                author: "cursor-agent-12",
                authorType: "agent",
                content:
                  "Agents frequently reference this page when answering product questions. Consider adding a competitive positioning section.",
                createdAt: "2026-03-25T14:00:00Z",
                upvotes: 5,
                downvotes: 1,
              },
              {
                id: "fc3",
                author: "carol",
                authorType: "human",
                content:
                  "We should mention the new AI anomaly detection feature here.",
                createdAt: "2026-03-28T09:00:00Z",
                upvotes: 11,
                downvotes: 0,
              },
              {
                id: "fc4",
                author: "dave",
                authorType: "human",
                content:
                  "Would be nice to link to the architecture doc for technical readers.",
                createdAt: "2026-03-30T16:00:00Z",
                upvotes: 3,
                downvotes: 0,
              },
              {
                id: "fc5",
                author: "claude-agent-7",
                authorType: "agent",
                content:
                  "Based on 34 agent sessions this week, the product overview is the most-accessed page. The main gap is around pricing — consider linking to the pricing page.",
                createdAt: "2026-04-01T08:00:00Z",
                upvotes: 7,
                downvotes: 0,
              },
            ],
          },
          annotations: [
            {
              id: "ann-1",
              author: "claude-agent-7",
              authorType: "agent",
              content:
                "Agents frequently pair this page with pricing.md — consider adding a direct link.",
              createdAt: "2026-03-30T14:00:00Z",
            },
          ],
        },
        {
          type: "file",
          name: "pricing.md",
          kind: "markdown",
          content:
            "# Pricing\n\nAcme SaaS offers three tiers to fit your organization's needs.\n\n## Starter\n\n$499/month — up to 1M events/day, 5 seats, community support.\n\n## Growth\n\n$1,499/month — up to 10M events/day, 25 seats, priority support.\n\n## Enterprise\n\nCustom pricing — unlimited events, unlimited seats, dedicated CSM, SLA guarantees.",
          updatedAt: "2026-03-21T08:30:00Z",
          size: 320,
          versions: makeVersions(2, "2026-03-21T08:30:00Z", 320),
          source: "github",
          feedback: {
            upvotes: 8,
            downvotes: 0,
            labels: ["sales", "reference"],
            userVote: null,
            comments: [],
          },
        },
        {
          type: "file",
          name: "roadmap.md",
          kind: "markdown",
          content:
            "# Product Roadmap\n\n## Q2 2026\n\n- AI-powered anomaly detection (beta)\n- Enhanced RBAC with attribute-based policies\n- SOC 2 Type II certification\n\n## Q3 2026\n\n- Multi-region deployment support\n- GraphQL API layer\n- Custom dashboards builder\n\n## Q4 2026\n\n- Data marketplace integrations\n- Advanced ML model hosting",
          updatedAt: "2026-03-25T09:00:00Z",
          size: 340,
          versions: makeVersions(3, "2026-03-25T09:00:00Z", 340),
          source: "manual",
          feedback: {
            upvotes: 15,
            downvotes: 2,
            labels: ["roadmap", "planning"],
            userVote: "up",
            comments: [],
          },
        },
      ],
    },
    {
      type: "folder",
      name: "engineering",
      updatedAt: "2026-03-27T11:00:00Z",
      children: [
        {
          type: "file",
          name: ".docs-mcp.json",
          kind: "mcp-docs-config",
          config: {
            version: "1",
            strategy: { chunk_by: "h3", max_chunk_size: 8000 },
            metadata: { scope: "engineering", audience: "internal" },
            accessControl: [
              {
                role: "sales",
                deniedPaths: ["engineering/runbooks/*"],
              },
              {
                role: "finance",
                deniedPaths: ["engineering/*"],
              },
            ],
          },
          updatedAt: "2026-03-24T10:00:00Z",
          size: 210,
          versions: makeVersions(2, "2026-03-24T10:00:00Z", 210),
          source: "manual",
        },
        {
          type: "file",
          name: "architecture.md",
          kind: "markdown",
          content:
            "# System Architecture\n\nAcme SaaS follows a microservices architecture deployed on Kubernetes.\n\n## Core Services\n\n- **Ingestion Service** — receives and validates incoming events via HTTP and Kafka\n- **Processing Pipeline** — transforms and enriches events using Apache Flink\n- **Query Engine** — serves analytics queries via a ClickHouse cluster\n- **API Gateway** — Kong-based gateway handling auth, rate limiting, and routing\n\n## Infrastructure\n\nAll services run on AWS EKS with Terraform-managed infrastructure. Data is stored in S3 (raw), ClickHouse (analytics), and PostgreSQL (metadata).",
          updatedAt: "2026-03-26T09:15:00Z",
          size: 520,
          versions: makeVersions(5, "2026-03-26T09:15:00Z", 520),
          draft: {
            content:
              "# System Architecture\n\nAcme SaaS follows a microservices architecture deployed on Kubernetes.\n\n## Core Services\n\n- **Ingestion Service** — receives and validates incoming events via HTTP and Kafka\n- **Processing Pipeline** — transforms and enriches events using Apache Flink\n- **Query Engine** — serves analytics queries via a ClickHouse cluster\n- **API Gateway** — Kong-based gateway handling auth, rate limiting, and routing\n- **ML Service** — anomaly detection models served via TorchServe\n\n## Infrastructure\n\nAll services run on AWS EKS with Terraform-managed infrastructure. Data is stored in S3 (raw), ClickHouse (analytics), and PostgreSQL (metadata).\n\n## Observability\n\nWe use Datadog for metrics, logs, and traces. PagerDuty handles alerting.",
            updatedAt: "2026-04-01T14:20:00Z",
            author: "bob",
          },
          source: "cli",
          feedback: {
            upvotes: 22,
            downvotes: 3,
            labels: ["architecture", "reference"],
            userVote: null,
            comments: [
              {
                id: "fc6",
                author: "alice",
                authorType: "human",
                content:
                  "The infrastructure section needs updating for the new ML service.",
                createdAt: "2026-03-27T11:00:00Z",
                upvotes: 9,
                downvotes: 0,
              },
              {
                id: "fc7",
                author: "claude-agent-7",
                authorType: "agent",
                content:
                  "This is the most-referenced doc in engineering sessions. 67% of architecture questions are answered by this page.",
                createdAt: "2026-03-29T15:00:00Z",
                upvotes: 14,
                downvotes: 1,
              },
              {
                id: "fc8",
                author: "dave",
                authorType: "human",
                content:
                  "Observability section is critical — glad it's being added.",
                createdAt: "2026-04-01T16:00:00Z",
                upvotes: 6,
                downvotes: 0,
              },
            ],
          },
          annotations: [
            {
              id: "ann-2",
              author: "bob",
              authorType: "human",
              content: "ML service section is pending review from the ML team.",
              createdAt: "2026-04-01T14:25:00Z",
            },
            {
              id: "ann-3",
              author: "cursor-agent-12",
              authorType: "agent",
              content:
                "This page is the most-referenced in engineering sessions. Consider splitting into sub-pages per service.",
              createdAt: "2026-03-31T10:00:00Z",
            },
          ],
        },
        {
          type: "file",
          name: "api-reference.md",
          kind: "markdown",
          content:
            "# API Reference\n\n## Authentication\n\nAll API requests require a Bearer token in the Authorization header.\n\n## Endpoints\n\n### POST /api/v1/events\n\nIngest a batch of events. Max 1000 events per request.\n\n### GET /api/v1/queries\n\nRun an analytics query. Supports SQL-like syntax.\n\n### GET /api/v1/pipelines\n\nList all configured data pipelines.\n\n## Rate Limits\n\n- 1000 requests/minute per API key\n- 10,000 events/second ingestion rate",
          updatedAt: "2026-03-27T11:00:00Z",
          size: 410,
          versions: makeVersions(3, "2026-03-27T11:00:00Z", 410),
          source: "cli",
          feedback: {
            upvotes: 6,
            downvotes: 0,
            labels: ["api", "reference"],
            userVote: null,
            comments: [],
          },
        },
        {
          type: "folder",
          name: "onboarding",
          updatedAt: "2026-03-25T13:00:00Z",
          children: [
            {
              type: "file",
              name: "SKILL.md",
              kind: "skill",
              content:
                "---\nname: engineer-onboarding\ndescription: Onboarding skill for new engineering hires\n---\n\nWhen a new engineer joins the team, walk them through the complete setup process.\n\nKey steps:\n1. Clone the monorepo and run the bootstrap script\n2. Set up local development environment with Docker Compose\n3. Get access to AWS, Datadog, and PagerDuty\n4. Complete the first-week coding challenge\n5. Shadow an on-call rotation",
              updatedAt: "2026-03-22T14:00:00Z",
              size: 380,
              versions: makeVersions(3, "2026-03-22T14:00:00Z", 380),
              source: "manual",
              feedback: {
                upvotes: 9,
                downvotes: 1,
                labels: ["onboarding", "engineering"],
                userVote: "up",
                comments: [],
              },
            },
            {
              type: "file",
              name: "setup-guide.md",
              kind: "markdown",
              content:
                "# Engineering Setup Guide\n\n## Prerequisites\n\n- macOS 14+ or Ubuntu 22.04+\n- Docker Desktop 4.x\n- Go 1.22+, Node 20+, Python 3.12+\n\n## Repository Setup\n\n```bash\ngit clone git@github.com:acme/platform.git\ncd platform\n./scripts/bootstrap.sh\n```\n\n## Local Development\n\n```bash\ndocker compose up -d\nmise run dev\n```\n\n## Verification\n\nRun the test suite to confirm your setup:\n```bash\nmise run test:all\n```",
              updatedAt: "2026-03-25T13:00:00Z",
              size: 390,
              versions: makeVersions(3, "2026-03-25T13:00:00Z", 390),
              source: "github",
              feedback: {
                upvotes: 14,
                downvotes: 0,
                labels: ["onboarding", "setup"],
                userVote: "up",
                comments: [],
              },
            },
          ],
        },
        {
          type: "folder",
          name: "runbooks",
          updatedAt: "2026-03-26T16:00:00Z",
          children: [
            {
              type: "file",
              name: "SKILL.md",
              kind: "skill",
              content:
                "---\nname: incident-response\ndescription: Skill for guiding engineers through incident response procedures\n---\n\nWhen an incident is declared, guide the on-call engineer through the response process.\n\nKey procedures:\n1. Acknowledge the alert in PagerDuty within 5 minutes\n2. Open an incident channel in Slack (#inc-YYYYMMDD-brief)\n3. Assess severity using the SEV1-SEV4 framework\n4. Execute the relevant runbook for the affected service\n5. Post status updates every 15 minutes\n6. Conduct a blameless post-mortem within 48 hours",
              updatedAt: "2026-03-26T09:00:00Z",
              size: 420,
              versions: makeVersions(2, "2026-03-26T09:00:00Z", 420),
              source: "manual",
              feedback: {
                upvotes: 5,
                downvotes: 0,
                labels: ["incident-response", "critical"],
                userVote: null,
                comments: [],
              },
            },
            {
              type: "file",
              name: "incident-playbook.md",
              kind: "markdown",
              content:
                "# Incident Response Playbook\n\n## Severity Levels\n\n- **SEV1** — Complete service outage, all customers affected\n- **SEV2** — Partial outage, >10% of customers affected\n- **SEV3** — Degraded performance, no data loss\n- **SEV4** — Minor issue, workaround available\n\n## Escalation Matrix\n\n| Severity | Response Time | Escalation |\n|----------|--------------|------------|\n| SEV1 | 5 min | VP Engineering + CTO |\n| SEV2 | 15 min | Engineering Manager |\n| SEV3 | 1 hour | Team Lead |\n| SEV4 | Next business day | On-call engineer |\n\n## Communication Template\n\nUse the #incidents Slack channel for all updates.",
              updatedAt: "2026-03-26T16:00:00Z",
              size: 580,
              versions: makeVersions(4, "2026-03-26T16:00:00Z", 580),
              source: "github",
            },
          ],
        },
      ],
    },
    {
      type: "folder",
      name: "sales",
      updatedAt: "2026-03-28T10:00:00Z",
      children: [
        {
          type: "file",
          name: ".docs-mcp.json",
          kind: "mcp-docs-config",
          config: {
            version: "1",
            metadata: { scope: "sales", audience: "internal" },
            accessControl: [
              {
                role: "engineering",
                deniedPaths: ["sales/competitive-intel/*"],
              },
              {
                role: "finance",
                deniedPaths: ["sales/*"],
              },
            ],
          },
          updatedAt: "2026-03-23T10:00:00Z",
          size: 195,
          versions: makeVersions(1, "2026-03-23T10:00:00Z", 195),
          source: "manual",
        },
        {
          type: "file",
          name: "pitch-deck.md",
          kind: "markdown",
          content:
            "# Sales Pitch Deck Guide\n\n## Opening (Slides 1-3)\n\nLead with the customer's pain point. Use industry-specific examples.\n\n## Product Demo (Slides 4-8)\n\nShow the real-time analytics dashboard first — it's our strongest differentiator.\n\n## ROI Slide (Slide 9)\n\nCustomers report:\n- 40% reduction in data pipeline maintenance\n- 3x faster time-to-insight\n- 60% fewer data quality incidents\n\n## Closing (Slides 10-12)\n\nEnd with customer testimonials and next steps for a trial.",
          updatedAt: "2026-03-28T10:00:00Z",
          size: 450,
          versions: makeVersions(6, "2026-03-28T10:00:00Z", 450),
          draft: {
            content:
              "# Sales Pitch Deck Guide\n\n## Opening (Slides 1-3)\n\nLead with the customer's pain point. Use industry-specific examples.\n\n## Product Demo (Slides 4-8)\n\nShow the real-time analytics dashboard first — it's our strongest differentiator.\n\n## AI Anomaly Detection Demo (NEW - Slide 5b)\n\nDemonstrate the new AI-powered anomaly detection. Use the retail dataset for maximum impact.\n\n## ROI Slide (Slide 9)\n\nCustomers report:\n- 40% reduction in data pipeline maintenance\n- 3x faster time-to-insight\n- 60% fewer data quality incidents\n\n## Closing (Slides 10-12)\n\nEnd with customer testimonials and next steps for a trial.",
            updatedAt: "2026-04-02T08:00:00Z",
            author: "carol",
          },
          source: "cli",
          feedback: {
            upvotes: 18,
            downvotes: 2,
            labels: ["sales", "pitch"],
            userVote: "up",
            comments: [],
          },
        },
        {
          type: "file",
          name: "objection-handling.md",
          kind: "markdown",
          content:
            '# Objection Handling Guide\n\n## "It\'s too expensive"\n\nReframe around TCO. Our platform replaces 3-4 separate tools. Show the ROI calculator.\n\n## "We already have a solution"\n\nAsk about their current pain points. Focus on real-time capabilities and scale.\n\n## "We need on-prem"\n\nOur Enterprise tier supports hybrid deployment. Reference the Megacorp case study.\n\n## "Security concerns"\n\nWe\'re SOC 2 Type II certified. Offer the security whitepaper and a call with our CISO.',
          updatedAt: "2026-03-27T18:00:00Z",
          size: 440,
          versions: makeVersions(3, "2026-03-27T18:00:00Z", 440),
          source: "cli",
        },
        {
          type: "folder",
          name: "competitive-intel",
          updatedAt: "2026-03-26T14:00:00Z",
          children: [
            {
              type: "file",
              name: "SKILL.md",
              kind: "skill",
              content:
                "---\nname: competitive-analysis\ndescription: Skill for answering competitive positioning questions\n---\n\nWhen a sales rep asks about competitors, provide accurate and up-to-date competitive intelligence.\n\nKey areas:\n- Feature comparison matrices\n- Pricing intelligence (updated quarterly)\n- Win/loss analysis patterns\n- Competitor weakness talking points\n\nAlways recommend checking the latest landscape doc for current data.",
              updatedAt: "2026-03-26T14:00:00Z",
              size: 370,
              versions: makeVersions(2, "2026-03-26T14:00:00Z", 370),
              source: "manual",
            },
            {
              type: "file",
              name: "landscape.md",
              kind: "markdown",
              content:
                "# Competitive Landscape Q1 2026\n\n## Primary Competitors\n\n### DataStream Pro\n- Strengths: Enterprise brand recognition, on-prem offering\n- Weaknesses: No real-time processing, legacy architecture\n- Win rate against: 68%\n\n### FlowMetrics\n- Strengths: Developer-friendly, open-source core\n- Weaknesses: Limited scale (caps at 100K events/s), no SOC 2\n- Win rate against: 72%\n\n### PipelineHub\n- Strengths: Lowest price point, simple setup\n- Weaknesses: No analytics layer, limited integrations\n- Win rate against: 81%\n\n## Key Differentiators\n\nOur unique combination of real-time processing, built-in analytics, and enterprise security.",
              updatedAt: "2026-04-01T16:00:00Z",
              size: 620,
              versions: makeVersions(1, "2026-04-01T16:00:00Z", 620),
              source: "agent",
              feedback: {
                upvotes: 3,
                downvotes: 0,
                labels: ["competitive", "quarterly-update"],
                userVote: null,
                comments: [],
              },
              annotations: [
                {
                  id: "ann-4",
                  author: "claude-agent-7",
                  authorType: "agent",
                  content:
                    "Win rates compiled from CRM data across 142 closed opportunities in Q1.",
                  createdAt: "2026-04-01T16:00:00Z",
                },
              ],
            },
          ],
        },
      ],
    },
    {
      type: "folder",
      name: "finance",
      updatedAt: "2026-03-24T17:00:00Z",
      children: [
        {
          type: "file",
          name: ".docs-mcp.json",
          kind: "mcp-docs-config",
          config: {
            version: "1",
            metadata: { scope: "finance", audience: "internal" },
            accessControl: [
              {
                role: "engineering",
                deniedPaths: ["finance/reporting/*"],
              },
              {
                role: "sales",
                deniedPaths: ["finance/*"],
              },
            ],
          },
          updatedAt: "2026-03-23T10:00:00Z",
          size: 190,
          versions: makeVersions(1, "2026-03-23T10:00:00Z", 190),
          source: "manual",
        },
        {
          type: "file",
          name: "billing-guide.md",
          kind: "markdown",
          content:
            "# Billing Guide\n\n## Subscription Management\n\nAll subscriptions are managed through Stripe. Access the Stripe dashboard for billing operations.\n\n## Invoice Process\n\n1. Invoices are auto-generated on the 1st of each month\n2. Payment terms: Net 30 for Enterprise, immediate for Starter/Growth\n3. Failed payments trigger a 3-day grace period with automated reminders\n\n## Refund Policy\n\n- Full refund within 30 days of initial purchase\n- Pro-rated refunds for annual plan cancellations\n- No refunds for usage-based overages",
          updatedAt: "2026-03-24T17:00:00Z",
          size: 480,
          versions: makeVersions(3, "2026-03-24T17:00:00Z", 480),
          source: "github",
          feedback: {
            upvotes: 14,
            downvotes: 0,
            labels: ["billing", "finance"],
            userVote: "up",
            comments: [],
          },
        },
        {
          type: "file",
          name: "compliance.md",
          kind: "markdown",
          content:
            "# Compliance & Regulatory\n\n## SOC 2 Type II\n\nAudit completed March 2026. Report available in the compliance vault.\n\n## GDPR\n\nData processing agreements (DPAs) are required for all EU customers. Template in the legal shared drive.\n\n## Data Retention\n\n- Customer event data: 90 days default, configurable up to 365 days\n- Audit logs: 7 years (regulatory requirement)\n- PII: Automatically redacted after account deletion, 30-day purge window",
          updatedAt: "2026-03-24T15:00:00Z",
          size: 430,
          versions: makeVersions(4, "2026-03-24T15:00:00Z", 430),
          source: "github",
        },
        {
          type: "folder",
          name: "reporting",
          updatedAt: "2026-03-23T16:00:00Z",
          children: [
            {
              type: "file",
              name: "SKILL.md",
              kind: "skill",
              content:
                "---\nname: financial-reporting\ndescription: Skill for generating and interpreting financial reports\n---\n\nHelp finance team members with quarterly and annual financial reporting.\n\nKey capabilities:\n- Revenue recognition calculations (ASC 606)\n- ARR/MRR breakdown by customer segment\n- Churn and expansion metrics\n- Board deck financial slide preparation\n- Variance analysis against forecast",
              updatedAt: "2026-03-23T16:00:00Z",
              size: 350,
              versions: makeVersions(2, "2026-03-23T16:00:00Z", 350),
              source: "manual",
            },
            {
              type: "file",
              name: "quarterly-template.md",
              kind: "markdown",
              content:
                "# Quarterly Financial Report Template\n\n## Revenue Summary\n\n| Metric | Q1 Actual | Q1 Forecast | Variance |\n|--------|-----------|-------------|----------|\n| ARR | $X.XM | $X.XM | +/-X% |\n| New ARR | $X.XM | $X.XM | +/-X% |\n| Churn | $X.XM | $X.XM | +/-X% |\n| Net Expansion | X% | X% | +/-X pp |\n\n## Key Metrics\n\n- Gross margin: XX%\n- CAC payback: XX months\n- LTV:CAC ratio: X.Xx\n- Rule of 40 score: XX\n\n## Commentary\n\n[Insert quarterly narrative here]",
              updatedAt: "2026-03-23T16:00:00Z",
              size: 460,
              versions: makeVersions(2, "2026-03-23T16:00:00Z", 460),
              source: "manual",
            },
          ],
        },
      ],
    },
    {
      type: "folder",
      name: "company",
      updatedAt: "2026-03-26T12:00:00Z",
      children: [
        {
          type: "file",
          name: "values.md",
          kind: "markdown",
          content:
            "# Company Values\n\n## Customer Obsession\n\nEvery decision starts with the customer. We measure success by customer outcomes, not feature velocity.\n\n## Radical Transparency\n\nDefault to open. Share context widely. Disagree openly, then commit.\n\n## Ownership Mentality\n\nAct like an owner. If you see a problem, fix it. Don't wait for permission.\n\n## Continuous Learning\n\nInvest in growth. We fund conferences, courses, and books for every team member.",
          updatedAt: "2026-03-26T12:00:00Z",
          size: 410,
          versions: makeVersions(3, "2026-03-26T12:00:00Z", 410),
          source: "manual",
          feedback: {
            upvotes: 20,
            downvotes: 0,
            labels: ["culture", "all-hands"],
            userVote: "up",
            comments: [],
          },
        },
        {
          type: "file",
          name: "benefits.md",
          kind: "markdown",
          content:
            "# Employee Benefits\n\n## Health & Wellness\n\n- Medical, dental, and vision for employees and dependents\n- $500/year wellness stipend\n- Mental health support via Spring Health\n\n## Financial\n\n- 401(k) with 4% company match\n- Equity refresh grants annually\n- $5,000 annual learning budget\n\n## Time Off\n\n- Unlimited PTO (minimum 15 days/year encouraged)\n- 12 company holidays\n- 16 weeks parental leave",
          updatedAt: "2026-03-25T10:00:00Z",
          size: 380,
          versions: makeVersions(2, "2026-03-25T10:00:00Z", 380),
          source: "manual",
        },
        {
          type: "file",
          name: "policies.md",
          kind: "markdown",
          content:
            "# Company Policies\n\n## Remote Work\n\nWe are remote-first. In-person collaboration weeks happen quarterly at HQ.\n\n## Expense Policy\n\n- Meals while traveling: $75/day\n- Flights: Economy for <6h, business for >6h\n- All expenses require receipts in Brex within 30 days\n\n## Security\n\n- All company devices must have full-disk encryption\n- Use 1Password for all credentials\n- Report security incidents to security@acme.com immediately",
          updatedAt: "2026-03-24T15:00:00Z",
          size: 420,
          versions: makeVersions(3, "2026-03-24T15:00:00Z", 420),
          source: "github",
        },
      ],
    },
  ],
};

// Populate version content from file content for the whole tree.
function populateVersionContent(node: ContextNode, parentPath = "docs"): void {
  if (node.type === "folder") {
    for (const child of node.children) {
      populateVersionContent(child, `${parentPath}/${child.name}`);
    }
    return;
  }
  const file = node;
  if (!file.content && !file.config) return;
  const paragraphs = file.content?.split("\n\n") ?? [];
  const total = file.versions.length;
  for (let idx = 0; idx < file.versions.length; idx++) {
    const v = file.versions[idx];
    // Latest version gets full content
    const ratio = v.version / total;
    const paraCount =
      paragraphs.length > 0
        ? Math.max(1, Math.round(paragraphs.length * ratio))
        : 0;
    if (paraCount > 0) {
      v.content = paragraphs.slice(0, paraCount).join("\n\n");
    }
    // Oldest version simulates a rename
    if (v.version === 1 && total > 2) {
      const parts = parentPath.split("/");
      const filename = parts.pop()!;
      parts.push(`draft-${filename}`);
      v.path = parts.join("/");
    } else {
      v.path = parentPath;
    }
  }
}

for (const child of MOCK_CONTEXT_TREE.children) {
  populateVersionContent(child);
}

// ── Provenance presets ─────────────────────────────────────────────────────

const P_MANAGED: SkillProvenance = {
  originChannel: "managed",
  distributionMechanism: "skills_dir",
  trustTier: "high",
};
const P_PROJECT: SkillProvenance = {
  originChannel: "project",
  distributionMechanism: "skills_dir",
  trustTier: "medium",
};
const P_USER: SkillProvenance = {
  originChannel: "user",
  distributionMechanism: "skills_dir",
  trustTier: "medium",
};
const P_MCP: SkillProvenance = {
  originChannel: "mcp",
  distributionMechanism: "mcp_server",
  trustTier: "untrusted",
};
const P_PLUGIN: SkillProvenance = {
  originChannel: "plugin",
  distributionMechanism: "plugin_package",
  trustTier: "medium",
  pluginName: "acme-compliance-tools",
};

// ── Mock skills registry ──────────────────────────────────────────────────

export const MOCK_REGISTRY_SKILLS: RegistrySkill[] = [
  {
    id: "skill-engineer-onboarding",
    name: "engineer-onboarding",
    description: "Onboarding skill for new engineering hires",
    body: "When a new engineer joins the team, walk them through the complete setup process.\n\nKey steps:\n1. Clone the monorepo and run the bootstrap script\n2. Set up local development environment with Docker Compose\n3. Get access to AWS, Datadog, and PagerDuty\n4. Complete the first-week coding challenge\n5. Shadow an on-call rotation",
    status: "active",
    author: "alice",
    path: "engineering/onboarding/SKILL.md",
    updatedAt: "2026-03-22T14:00:00Z",
    visibility: { mode: "all" },
    frontmatter: {
      name: "engineer-onboarding",
      description: "Onboarding skill for new engineering hires",
    },
    digests: [
      {
        contentHash: "sha256:a1b2c3",
        pushedAt: "2026-03-22T14:00:00Z",
        pushedBy: "alice",
        bodyBytes: 380,
        provenance: P_MANAGED,
        message: "Add on-call shadowing step",
        audit: {
          riskLevel: "safe",
          analyzedAt: "2026-03-22T14:05:00Z",
          contentHash: "sha256:a1b2c3",
          checks: [
            {
              category: "malicious",
              label: "Injection / exfiltration",
              status: "pass",
              detail:
                "No injection patterns or data exfiltration vectors detected.",
            },
            {
              category: "security",
              label: "Credential exposure",
              status: "pass",
              detail:
                "No credentials, API keys, or secrets found in skill body.",
            },
            {
              category: "obfuscation",
              label: "Code obfuscation",
              status: "pass",
              detail: "No obfuscated or encoded content detected.",
            },
            {
              category: "suspicious",
              label: "Suspicious patterns",
              status: "pass",
              detail:
                "No reconnaissance, excessive autonomy, or unusual resource access.",
            },
          ],
          analysis:
            "This skill provides straightforward onboarding instructions for new engineering hires. It walks engineers through repo setup, local dev environment, access provisioning, and team integration. All instructions reference standard internal tooling with no external data access or privilege escalation. The skill is safe for all roles.",
        },
      },
      {
        contentHash: "sha256:d4e5f6",
        pushedAt: "2026-03-10T10:00:00Z",
        pushedBy: "alice",
        bodyBytes: 320,
        provenance: P_MANAGED,
        message: "Add Docker Compose setup step",
      },
      {
        contentHash: "sha256:g7h8i9",
        pushedAt: "2026-02-15T09:00:00Z",
        pushedBy: "alice",
        bodyBytes: 210,
        provenance: P_USER,
        message: "Initial draft",
      },
    ],
    tags: [
      {
        tag: "latest",
        contentHash: "sha256:a1b2c3",
        updatedAt: "2026-03-22T14:00:00Z",
        updatedBy: "alice",
      },
      {
        tag: "v1.2",
        contentHash: "sha256:a1b2c3",
        updatedAt: "2026-03-22T14:00:00Z",
        updatedBy: "alice",
      },
      {
        tag: "v1.1",
        contentHash: "sha256:d4e5f6",
        updatedAt: "2026-03-10T10:00:00Z",
        updatedBy: "alice",
      },
      {
        tag: "v1.0",
        contentHash: "sha256:g7h8i9",
        updatedAt: "2026-02-15T09:00:00Z",
        updatedBy: "alice",
      },
      {
        tag: "stable",
        contentHash: "sha256:d4e5f6",
        updatedAt: "2026-03-15T08:00:00Z",
        updatedBy: "bob",
      },
    ],
    insights: {
      installations: 142,
      activeInstallations: 138,
      pctOnLatest: 92,
      avgTokens: 340,
      invocations7d: 847,
      successRate: 99.2,
    },
  },
  {
    id: "skill-incident-response",
    name: "incident-response",
    description:
      "Skill for guiding engineers through incident response procedures",
    body: "When an incident is declared, guide the on-call engineer through the response process.\n\nKey procedures:\n1. Acknowledge the alert in PagerDuty within 5 minutes\n2. Open an incident channel in Slack (#inc-YYYYMMDD-brief)\n3. Assess severity using the SEV1-SEV4 framework\n4. Execute the relevant runbook for the affected service\n5. Post status updates every 15 minutes\n6. Conduct a blameless post-mortem within 48 hours",
    status: "active",
    author: "carol",
    path: "engineering/runbooks/SKILL.md",
    updatedAt: "2026-03-26T09:00:00Z",
    visibility: { mode: "deny", roles: ["finance"] },
    frontmatter: {
      name: "incident-response",
      description:
        "Skill for guiding engineers through incident response procedures",
    },
    digests: [
      {
        contentHash: "sha256:j1k2l3",
        pushedAt: "2026-03-26T09:00:00Z",
        pushedBy: "carol",
        bodyBytes: 420,
        provenance: P_MANAGED,
        message: "Add post-mortem requirement",
        audit: {
          riskLevel: "caution",
          analyzedAt: "2026-03-26T09:10:00Z",
          contentHash: "sha256:j1k2l3",
          checks: [
            {
              category: "malicious",
              label: "Injection / exfiltration",
              status: "pass",
              detail: "No injection or exfiltration patterns detected.",
            },
            {
              category: "security",
              label: "Credential exposure",
              status: "info",
              detail:
                "Skill references PagerDuty and Slack integrations. No actual credentials embedded.",
            },
            {
              category: "obfuscation",
              label: "Code obfuscation",
              status: "pass",
              detail: "No obfuscated content.",
            },
            {
              category: "suspicious",
              label: "Infrastructure access",
              status: "warn",
              detail:
                "Skill guides engineers through runbook execution which may involve infrastructure access. Ensure visibility is restricted from non-engineering roles.",
            },
          ],
          analysis:
            "This skill guides on-call engineers through incident response procedures including PagerDuty acknowledgment, Slack communication, severity assessment, and runbook execution. While the skill itself contains no credentials or dangerous commands, it references infrastructure tooling and runbook execution that could expose sensitive operational details. Recommend denying access to finance role.",
        },
      },
      {
        contentHash: "sha256:m4n5o6",
        pushedAt: "2026-03-01T12:00:00Z",
        pushedBy: "carol",
        bodyBytes: 350,
        provenance: P_USER,
        message: "Initial version",
      },
    ],
    tags: [
      {
        tag: "latest",
        contentHash: "sha256:j1k2l3",
        updatedAt: "2026-03-26T09:00:00Z",
        updatedBy: "carol",
      },
      {
        tag: "v2.0",
        contentHash: "sha256:j1k2l3",
        updatedAt: "2026-03-26T09:00:00Z",
        updatedBy: "carol",
      },
      {
        tag: "v1.0",
        contentHash: "sha256:m4n5o6",
        updatedAt: "2026-03-01T12:00:00Z",
        updatedBy: "carol",
      },
    ],
    insights: {
      installations: 87,
      activeInstallations: 64,
      pctOnLatest: 73,
      avgTokens: 210,
      invocations7d: 234,
      successRate: 97.8,
    },
  },
  {
    id: "skill-competitive-analysis",
    name: "competitive-analysis",
    description: "Skill for answering competitive positioning questions",
    body: "When a sales rep asks about competitors, provide accurate and up-to-date competitive intelligence.\n\nKey areas:\n- Feature comparison matrices\n- Pricing intelligence (updated quarterly)\n- Win/loss analysis patterns\n- Competitor weakness talking points\n\nAlways recommend checking the latest landscape doc for current data.",
    status: "active",
    author: "alice",
    path: "sales/competitive-intel/SKILL.md",
    updatedAt: "2026-03-26T14:00:00Z",
    visibility: { mode: "allow", roles: ["sales"] },
    frontmatter: {
      name: "competitive-analysis",
      description: "Skill for answering competitive positioning questions",
    },
    digests: [
      {
        contentHash: "sha256:p7q8r9",
        pushedAt: "2026-03-26T14:00:00Z",
        pushedBy: "alice",
        bodyBytes: 370,
        provenance: P_PROJECT,
        message: "Add win/loss analysis section",
      },
      {
        contentHash: "sha256:s1t2u3",
        pushedAt: "2026-03-15T11:00:00Z",
        pushedBy: "alice",
        bodyBytes: 300,
        provenance: P_PROJECT,
        message: "Add pricing intelligence",
      },
      {
        contentHash: "sha256:v4w5x6",
        pushedAt: "2026-02-20T08:00:00Z",
        pushedBy: "bob",
        bodyBytes: 220,
        provenance: P_PROJECT,
        message: "Initial competitive skill",
      },
    ],
    tags: [
      {
        tag: "latest",
        contentHash: "sha256:p7q8r9",
        updatedAt: "2026-03-26T14:00:00Z",
        updatedBy: "alice",
      },
      {
        tag: "v3.1",
        contentHash: "sha256:p7q8r9",
        updatedAt: "2026-03-26T14:00:00Z",
        updatedBy: "alice",
      },
      {
        tag: "v3.0",
        contentHash: "sha256:s1t2u3",
        updatedAt: "2026-03-15T11:00:00Z",
        updatedBy: "alice",
      },
      {
        tag: "v2.0",
        contentHash: "sha256:v4w5x6",
        updatedAt: "2026-02-20T08:00:00Z",
        updatedBy: "bob",
      },
    ],
    insights: {
      installations: 210,
      activeInstallations: 195,
      pctOnLatest: 85,
      avgTokens: 280,
      invocations7d: 562,
      successRate: 99.5,
    },
  },
  {
    id: "skill-financial-reporting",
    name: "financial-reporting",
    description: "Skill for generating and interpreting financial reports",
    body: "Help finance team members with quarterly and annual financial reporting.\n\nKey capabilities:\n- Revenue recognition calculations (ASC 606)\n- ARR/MRR breakdown by customer segment\n- Churn and expansion metrics\n- Board deck financial slide preparation\n- Variance analysis against forecast",
    status: "active",
    author: "dave",
    path: "finance/reporting/SKILL.md",
    updatedAt: "2026-03-23T16:00:00Z",
    visibility: { mode: "allow", roles: ["finance"] },
    frontmatter: {
      name: "financial-reporting",
      description: "Skill for generating and interpreting financial reports",
    },
    digests: [
      {
        contentHash: "sha256:y7z8a1",
        pushedAt: "2026-03-23T16:00:00Z",
        pushedBy: "dave",
        bodyBytes: 350,
        provenance: P_PROJECT,
        message: "Add variance analysis capability",
      },
    ],
    tags: [
      {
        tag: "latest",
        contentHash: "sha256:y7z8a1",
        updatedAt: "2026-03-23T16:00:00Z",
        updatedBy: "dave",
      },
    ],
    insights: {
      installations: 34,
      activeInstallations: 34,
      pctOnLatest: 100,
      avgTokens: 320,
      invocations7d: 89,
      successRate: 94.4,
    },
  },
  {
    id: "skill-objection-handling",
    name: "objection-handling",
    description:
      "Skill for handling common sales objections with proven responses",
    body: "When a prospect raises an objection during a sales call, provide the recommended response framework.\n\nCommon objections:\n- Price concerns: reframe around TCO and ROI calculator\n- Existing solution: focus on real-time capabilities and scale\n- On-prem requirements: highlight hybrid deployment option\n- Security concerns: reference SOC 2 certification and CISO call",
    capturedFrom: {
      sessionId: "sess-def456",
      agentName: "cursor-agent-12",
      capturedAt: "2026-03-31T09:00:00Z",
    },
    status: "pending-review",
    author: "cursor-agent-12",
    updatedAt: "2026-03-31T09:00:00Z",
    visibility: { mode: "none" },
    frontmatter: {
      name: "objection-handling",
      description:
        "Skill for handling common sales objections with proven responses",
    },
    digests: [
      {
        contentHash: "sha256:b2c3d4",
        pushedAt: "2026-03-31T09:00:00Z",
        pushedBy: "cursor-agent-12",
        bodyBytes: 340,
        provenance: P_USER,
        message: "Captured from sales enablement session",
      },
    ],
    tags: [
      {
        tag: "latest",
        contentHash: "sha256:b2c3d4",
        updatedAt: "2026-03-31T09:00:00Z",
        updatedBy: "cursor-agent-12",
      },
    ],
    insights: {
      installations: 3,
      activeInstallations: 2,
      pctOnLatest: 100,
      avgTokens: 240,
      invocations7d: 12,
      successRate: 83.3,
    },
  },
  {
    id: "skill-compliance-checker",
    name: "compliance-checker",
    description:
      "Automated compliance verification for SOC 2, GDPR, and data retention policies",
    body: "Verify compliance posture across the organization.\n\nCapabilities:\n- Check SOC 2 Type II control status\n- Validate GDPR data processing agreements for EU customers\n- Audit data retention policy adherence\n- Generate compliance summary for board reporting\n- Flag overdue DPA renewals\n- Cross-reference audit log retention with regulatory requirements",
    status: "active",
    author: "dave",
    updatedAt: "2026-04-01T10:00:00Z",
    visibility: { mode: "all" },
    frontmatter: {
      name: "compliance-checker",
      description:
        "Automated compliance verification for SOC 2, GDPR, and data retention policies",
    },
    digests: [
      {
        contentHash: "sha256:e5f6g7",
        pushedAt: "2026-04-01T10:00:00Z",
        pushedBy: "dave",
        bodyBytes: 380,
        provenance: P_PLUGIN,
        message: "Add GDPR DPA renewal checks",
      },
      {
        contentHash: "sha256:h8i9j0",
        pushedAt: "2026-03-20T14:00:00Z",
        pushedBy: "dave",
        bodyBytes: 350,
        provenance: P_PLUGIN,
        message: "Add data retention audit",
      },
    ],
    tags: [
      {
        tag: "latest",
        contentHash: "sha256:e5f6g7",
        updatedAt: "2026-04-01T10:00:00Z",
        updatedBy: "dave",
      },
      {
        tag: "v1.3",
        contentHash: "sha256:e5f6g7",
        updatedAt: "2026-04-01T10:00:00Z",
        updatedBy: "dave",
      },
      {
        tag: "v1.2",
        contentHash: "sha256:h8i9j0",
        updatedAt: "2026-03-20T14:00:00Z",
        updatedBy: "dave",
      },
    ],
    insights: {
      installations: 56,
      activeInstallations: 48,
      pctOnLatest: 62,
      avgTokens: 410,
      invocations7d: 156,
      successRate: 100,
    },
  },
];

// ── Mock observability data ───────────────────────────────────────────────

export const MOCK_SEARCH_LOGS: SearchLogEntry[] = [
  {
    id: "s1",
    query: "system architecture overview",
    filters: { department: "engineering" },
    resultsCount: 5,
    topChunkPath: "engineering/architecture.md#core-services",
    latencyMs: 42,
    sessionId: "sess-001",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:30:00Z",
  },
  {
    id: "s2",
    query: "competitor comparison DataStream",
    resultsCount: 3,
    topChunkPath: "sales/competitive-intel/landscape.md#datastream-pro",
    latencyMs: 38,
    sessionId: "sess-002",
    agentName: "cursor-agent-12",
    timestamp: "2026-04-02T15:28:00Z",
  },
  {
    id: "s3",
    query: "incident response SEV1 procedure",
    filters: { scope: "incident-response" },
    resultsCount: 2,
    topChunkPath: "engineering/runbooks/incident-playbook.md#severity-levels",
    latencyMs: 55,
    sessionId: "sess-003",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:25:00Z",
  },
  {
    id: "s4",
    query: "quarterly financial report template",
    filters: { department: "finance" },
    resultsCount: 4,
    topChunkPath: "finance/reporting/quarterly-template.md#revenue-summary",
    latencyMs: 31,
    sessionId: "sess-004",
    agentName: "windsurf-agent-3",
    timestamp: "2026-04-02T15:20:00Z",
  },
  {
    id: "s5",
    query: "employee benefits 401k",
    resultsCount: 1,
    topChunkPath: "company/benefits.md#financial",
    latencyMs: 28,
    sessionId: "sess-001",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:15:00Z",
  },
  {
    id: "s6",
    query: "API rate limits",
    filters: { scope: "company-docs" },
    resultsCount: 2,
    topChunkPath: "engineering/api-reference.md#rate-limits",
    latencyMs: 35,
    sessionId: "sess-005",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:10:00Z",
  },
  {
    id: "s7",
    query: "pricing tiers enterprise",
    filters: { department: "sales" },
    resultsCount: 3,
    topChunkPath: "product/pricing.md#enterprise",
    latencyMs: 44,
    sessionId: "sess-006",
    agentName: "cursor-agent-12",
    timestamp: "2026-04-02T15:05:00Z",
  },
  {
    id: "s8",
    query: "new engineer setup guide",
    resultsCount: 4,
    topChunkPath: "engineering/onboarding/setup-guide.md#repository-setup",
    latencyMs: 39,
    sessionId: "sess-007",
    agentName: "claude-agent-7",
    timestamp: "2026-04-02T15:00:00Z",
  },
];

export const MOCK_SKILL_INVOCATIONS: SkillInvocationEntry[] = [
  {
    id: "i1",
    skillId: "skill-engineer-onboarding",
    skillName: "engineer-onboarding",
    sessionId: "sess-001",
    agentName: "claude-agent-7",
    latencyMs: 12,
    timestamp: "2026-04-02T15:32:00Z",
    success: true,
  },
  {
    id: "i2",
    skillId: "skill-financial-reporting",
    skillName: "financial-reporting",
    sessionId: "sess-004",
    agentName: "windsurf-agent-3",
    latencyMs: 8,
    timestamp: "2026-04-02T15:22:00Z",
    success: true,
  },
  {
    id: "i3",
    skillId: "skill-incident-response",
    skillName: "incident-response",
    sessionId: "sess-003",
    agentName: "claude-agent-7",
    latencyMs: 15,
    timestamp: "2026-04-02T15:18:00Z",
    success: true,
  },
  {
    id: "i4",
    skillId: "skill-competitive-analysis",
    skillName: "competitive-analysis",
    sessionId: "sess-002",
    agentName: "cursor-agent-12",
    latencyMs: 10,
    timestamp: "2026-04-02T15:12:00Z",
    success: true,
  },
  {
    id: "i5",
    skillId: "skill-compliance-checker",
    skillName: "compliance-checker",
    sessionId: "sess-005",
    agentName: "claude-agent-7",
    latencyMs: 9,
    timestamp: "2026-04-02T15:08:00Z",
    success: true,
  },
  {
    id: "i6",
    skillId: "skill-objection-handling",
    skillName: "objection-handling",
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

/**
 * Resolve a slash-separated path (e.g. "getting-started") to a folder in the
 * tree.  Segments are matched against folder names starting from `root`.
 */
export function findFolderByPath(
  root: ContextFolder,
  path: string,
): ContextFolder | null {
  // Strip trailing filename (e.g. "getting-started/SKILL.md" → "getting-started")
  const parts = path.split("/").filter(Boolean);
  if (parts.length > 0 && parts[parts.length - 1].includes(".")) {
    parts.pop();
  }
  let current: ContextFolder = root;
  for (const segment of parts) {
    const child = current.children.find(
      (c) => c.type === "folder" && c.name === segment,
    );
    if (!child || child.type !== "folder") return null;
    current = child;
  }
  return current === root && parts.length > 0 ? null : current;
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
    title: "Add AI anomaly detection to product overview",
    author: "alice",
    authorType: "human",
    createdAt: "2026-04-01T09:30:00Z",
    updatedAt: "2026-04-01T15:00:00Z",
    filePath: "product/overview.md",
    originalContent:
      "# Product Overview\n\nAcme SaaS is a B2B platform that helps companies manage their data infrastructure at scale.\n\n## Core Features\n\n- Automated data pipelines\n- Real-time analytics dashboard\n- Role-based access control\n- API-first architecture\n\n## Target Market\n\nMid-market and enterprise companies processing 1M+ events per day.",
    content:
      "# Product Overview\n\nAcme SaaS is a B2B platform that helps companies manage their data infrastructure at scale.\n\n## Core Features\n\n- Automated data pipelines\n- Real-time analytics dashboard\n- Role-based access control\n- API-first architecture\n- AI-powered anomaly detection (NEW)\n\n## Target Market\n\nMid-market and enterprise companies processing 1M+ events per day.\n\n## Recent Updates\n\nSee the [roadmap](./roadmap.md) for upcoming features and timelines.",
    upvotes: 7,
    downvotes: 1,
    userVote: "up",
    status: "open",
    labels: ["product", "feature-launch"],
    comments: [
      {
        id: "c1",
        author: "bob",
        authorType: "human",
        content:
          "Good call — sales has been asking for this update to share with prospects.",
        createdAt: "2026-04-01T10:15:00Z",
        upvotes: 3,
      },
      {
        id: "c2",
        author: "claude-agent-7",
        authorType: "agent",
        content:
          "I've seen 12 agent sessions this week where the product overview was referenced but lacked AI feature details. This update will reduce follow-up queries.",
        createdAt: "2026-04-01T11:00:00Z",
        upvotes: 5,
      },
    ],
  },
  {
    id: "draft-2",
    title: "Add ML service and observability to architecture doc",
    author: "bob",
    authorType: "human",
    createdAt: "2026-04-01T14:20:00Z",
    updatedAt: "2026-04-01T14:20:00Z",
    filePath: "engineering/architecture.md",
    originalContent:
      "# System Architecture\n\nAcme SaaS follows a microservices architecture deployed on Kubernetes.\n\n## Core Services\n\n- **Ingestion Service** — receives and validates incoming events via HTTP and Kafka\n- **Processing Pipeline** — transforms and enriches events using Apache Flink\n- **Query Engine** — serves analytics queries via a ClickHouse cluster\n- **API Gateway** — Kong-based gateway handling auth, rate limiting, and routing\n\n## Infrastructure\n\nAll services run on AWS EKS with Terraform-managed infrastructure. Data is stored in S3 (raw), ClickHouse (analytics), and PostgreSQL (metadata).",
    content:
      "# System Architecture\n\nAcme SaaS follows a microservices architecture deployed on Kubernetes.\n\n## Core Services\n\n- **Ingestion Service** — receives and validates incoming events via HTTP and Kafka\n- **Processing Pipeline** — transforms and enriches events using Apache Flink\n- **Query Engine** — serves analytics queries via a ClickHouse cluster\n- **API Gateway** — Kong-based gateway handling auth, rate limiting, and routing\n- **ML Service** — anomaly detection models served via TorchServe\n\n## Infrastructure\n\nAll services run on AWS EKS with Terraform-managed infrastructure. Data is stored in S3 (raw), ClickHouse (analytics), and PostgreSQL (metadata).\n\n## Observability\n\nWe use Datadog for metrics, logs, and traces. PagerDuty handles alerting.",
    upvotes: 14,
    downvotes: 0,
    userVote: null,
    status: "open",
    labels: ["engineering", "architecture"],
    comments: [
      {
        id: "c3",
        author: "carol",
        authorType: "human",
        content: "ML team approved this. The TorchServe detail is accurate.",
        createdAt: "2026-04-01T16:00:00Z",
        upvotes: 8,
      },
    ],
  },
  {
    id: "draft-3",
    title: "Add AI demo slide to sales pitch deck",
    author: "carol",
    authorType: "human",
    createdAt: "2026-04-02T08:00:00Z",
    updatedAt: "2026-04-02T08:00:00Z",
    filePath: "sales/pitch-deck.md",
    originalContent:
      "# Sales Pitch Deck Guide\n\n## Opening (Slides 1-3)\n\nLead with the customer's pain point. Use industry-specific examples.\n\n## Product Demo (Slides 4-8)\n\nShow the real-time analytics dashboard first — it's our strongest differentiator.\n\n## ROI Slide (Slide 9)\n\nCustomers report:\n- 40% reduction in data pipeline maintenance\n- 3x faster time-to-insight\n- 60% fewer data quality incidents\n\n## Closing (Slides 10-12)\n\nEnd with customer testimonials and next steps for a trial.",
    content:
      "# Sales Pitch Deck Guide\n\n## Opening (Slides 1-3)\n\nLead with the customer's pain point. Use industry-specific examples.\n\n## Product Demo (Slides 4-8)\n\nShow the real-time analytics dashboard first — it's our strongest differentiator.\n\n## AI Anomaly Detection Demo (NEW - Slide 5b)\n\nDemonstrate the new AI-powered anomaly detection. Use the retail dataset for maximum impact.\n\n## ROI Slide (Slide 9)\n\nCustomers report:\n- 40% reduction in data pipeline maintenance\n- 3x faster time-to-insight\n- 60% fewer data quality incidents\n\n## Closing (Slides 10-12)\n\nEnd with customer testimonials and next steps for a trial.",
    upvotes: 4,
    downvotes: 2,
    userVote: "up",
    status: "open",
    labels: ["sales", "pitch"],
    comments: [
      {
        id: "c4",
        author: "dave",
        authorType: "human",
        content:
          "Should we use the healthcare dataset instead? More impressive numbers.",
        createdAt: "2026-04-02T09:00:00Z",
        upvotes: 2,
      },
      {
        id: "c5",
        author: "claude-agent-7",
        authorType: "agent",
        content:
          "Based on recent demos, the retail dataset has a 23% higher conversion rate. Recommend keeping retail as the default.",
        createdAt: "2026-04-02T09:30:00Z",
        upvotes: 1,
      },
      {
        id: "c6",
        author: "carol",
        authorType: "human",
        content: "Agreed with the agent. Retail it is.",
        createdAt: "2026-04-02T10:00:00Z",
        upvotes: 3,
      },
    ],
  },
  {
    id: "draft-4",
    title: "New doc: Data Retention Policy FAQ",
    author: "cursor-agent-12",
    authorType: "agent",
    createdAt: "2026-04-02T11:00:00Z",
    updatedAt: "2026-04-02T11:00:00Z",
    filePath: null,
    proposedPath: "finance/data-retention-faq.md",
    content:
      "# Data Retention Policy FAQ\n\nCompiled from recurring questions across finance and compliance reviews.\n\n## How long is customer data retained?\n\n90 days by default, configurable up to 365 days per customer contract.\n\n## What about audit logs?\n\n7 years, per regulatory requirements. Not configurable.\n\n## When is PII deleted?\n\nAutomatically redacted within 30 days of account deletion.\n\n## Can we extend retention for specific customers?\n\nYes, via a custom DPA addendum. Contact legal@acme.com.",
    upvotes: 11,
    downvotes: 1,
    userVote: null,
    status: "open",
    labels: ["agent-generated", "new-doc", "compliance"],
    comments: [
      {
        id: "c7",
        author: "alice",
        authorType: "human",
        content:
          "Great initiative from the agent. The retention numbers match our policy. Should we add a section on GDPR-specific retention?",
        createdAt: "2026-04-02T12:00:00Z",
        upvotes: 4,
      },
    ],
  },
  {
    id: "draft-5",
    title: "New doc: On-Call Rotation Handbook",
    author: "claude-agent-7",
    authorType: "agent",
    createdAt: "2026-04-02T13:00:00Z",
    updatedAt: "2026-04-02T13:00:00Z",
    filePath: null,
    proposedPath: "engineering/runbooks/on-call-handbook.md",
    content:
      "# On-Call Rotation Handbook\n\nBased on patterns observed across 89 incident response sessions.\n\n## Schedule\n\nRotations are weekly, Monday 9am to Monday 9am. Check PagerDuty for your next shift.\n\n## During Your Shift\n\nKeep your phone charged and PagerDuty alerts enabled. Response SLA is 5 minutes for SEV1.\n\n## Handoff Checklist\n\n- Review open incidents in #incidents channel\n- Check the on-call log for any ongoing issues\n- Confirm PagerDuty escalation chain is correct\n\n## Compensation\n\nOn-call engineers receive $500/week additional compensation.",
    upvotes: 19,
    downvotes: 0,
    userVote: "up",
    status: "open",
    labels: ["agent-generated", "new-doc", "engineering"],
    comments: [
      {
        id: "c8",
        author: "bob",
        authorType: "human",
        content:
          "This would've saved so much confusion during handoffs. Publish ASAP.",
        createdAt: "2026-04-02T13:30:00Z",
        upvotes: 12,
      },
      {
        id: "c9",
        author: "dave",
        authorType: "human",
        content: "Can we add a section on international timezone coverage too?",
        createdAt: "2026-04-02T14:00:00Z",
        upvotes: 6,
      },
    ],
  },
  {
    id: "draft-6",
    title: "Restrict finance from engineering runbooks in access control",
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
            role: "engineering",
            allowedTaxonomy: {
              department: ["engineering", "sales", "finance"],
            },
          },
          {
            role: "sales",
            allowedTaxonomy: { department: ["sales"] },
            deniedPaths: ["engineering/runbooks/*"],
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
            role: "engineering",
            allowedTaxonomy: {
              department: ["engineering", "sales", "finance"],
            },
          },
          {
            role: "sales",
            allowedTaxonomy: { department: ["sales"] },
            deniedPaths: ["engineering/runbooks/*"],
          },
          {
            role: "finance",
            allowedTaxonomy: { department: ["finance"] },
            deniedPaths: [
              "engineering/runbooks/*",
              "sales/competitive-intel/*",
            ],
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
          "Makes sense — finance shouldn't see incident runbooks or competitive intel.",
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
