import { useInfiniteQuery } from "@tanstack/react-query";

const PAGE_SIZE = 10;

interface ServerMeta {
  "com.pulsemcp/server"?: {
    visitorsEstimateMostRecentWeek?: number;
    visitorsEstimateLastFourWeeks?: number;
    visitorsEstimateTotal?: number;
    isOfficial?: boolean;
  };
  "com.pulsemcp/server-version"?: {
    source?: string;
    status?: string;
    publishedAt?: string;
    updatedAt?: string;
    isLatest?: boolean;
  };
}

export interface Server {
  name: string;
  version: string;
  description: string;
  registry_id: string;
  title: string;
  logo: string;
  meta: ServerMeta;
}

interface ListRegistriesResponse {
  servers: Server[];
}

const SampleServers = {
  servers: [
    {
      name: "io.github.hashicorp/terraform-mcp-server",
      version: "0.3.3",
      description:
        "Generate more accurate Terraform and automate workflows for HCP Terraform and Terraform Enterprise",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "Terraform",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-11-21T21:31:28.121089Z",
          updatedAt: "2025-11-30T17:43:41Z",
          isLatest: true,
        },
      },
    },
    {
      name: "com.pulsemcp.mirror/dbt-cloud",
      version: "0.0.1",
      description:
        "Remote MCP server for dbt Cloud AI features including Semantic Layer, SQL, and Discovery tools",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "dbt Cloud",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateLastFourWeeks: 97638,
          visitorsEstimateTotal: 618816,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "pulsemcp.com",
          status: "active",
          publishedAt: "2025-11-29T22:49:16Z",
          updatedAt: "2025-12-04T02:02:01Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.Snowflake-Labs/mcp",
      version: "1.3.5",
      description: "MCP Server for Snowflake from Snowflake Labs",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "OSS Snowflake MCP Server",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-10-28T15:19:33.176334Z",
          updatedAt: "2025-12-05T19:48:44Z",
          isLatest: true,
        },
      },
    },
    {
      name: "com.pulsemcp.mirror/genai-toolbox",
      version: "0.22.0",
      description:
        "MCP server for databases with connection pooling, authentication, and OpenTelemetry support.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "MCP Toolbox for Databases",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 21947,
          visitorsEstimateLastFourWeeks: 85072,
          visitorsEstimateTotal: 361200,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "pulsemcp.com",
          status: "active",
          publishedAt: "2025-12-05T19:57:26Z",
          updatedAt: "2025-12-05T21:41:49Z",
          isLatest: true,
        },
      },
    },
    {
      name: "com.pulsemcp.mirror/snowflake",
      version: "1.3.5",
      description: "MCP Server for Snowflake from Snowflake Labs",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "OSS Snowflake MCP Server",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "pulsemcp.com",
          status: "active",
          publishedAt: "2025-12-05T21:48:55Z",
          updatedAt: "2025-12-05T21:50:37Z",
          isLatest: true,
        },
      },
    },
    {
      name: "com.notion/mcp",
      version: "1.0.1",
      description: "Official Notion MCP server",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      logo: "/external/sticker-logo.png",
      title: "Notion MCP Server",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 6620,
          visitorsEstimateLastFourWeeks: 26480,
          visitorsEstimateTotal: 62989,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-09-11T22:25:50.737872Z",
          updatedAt: "2025-12-07T21:34:08Z",
          isLatest: true,
        },
      },
    },
    {
      name: "com.atlassian/atlassian-mcp-server",
      version: "1.0.0",
      description:
        "Enables secure, permission-aware access to Atlassian Cloud products.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "Atlassian Rovo MCP Server",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 8274,
          visitorsEstimateLastFourWeeks: 31442,
          visitorsEstimateTotal: 78158,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-10-22T01:37:12.669264Z",
          updatedAt: "2025-12-07T22:01:08Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
    {
      name: "io.github.github/github-mcp-server",
      version: "0.24.1",
      description:
        "Connect AI assistants to GitHub - manage repos, issues, PRs, and workflows through natural language.",
      registry_id: "019aea79-fb9b-743f-9437-4ec358d01da8",
      title: "GitHub",
      logo: "/external/sticker-logo.png",
      meta: {
        "com.pulsemcp/server": {
          visitorsEstimateMostRecentWeek: 7693,
          visitorsEstimateLastFourWeeks: 29978,
          visitorsEstimateTotal: 257673,
          isOfficial: true,
        },
        "com.pulsemcp/server-version": {
          source: "registry.modelcontextprotocol.io",
          status: "active",
          publishedAt: "2025-12-08T10:19:24.868255Z",
          updatedAt: "2025-12-09T02:00:04Z",
          isLatest: true,
        },
      },
    },
  ],
};

interface PaginatedResponse {
  servers: Server[];
  nextCursor: number | null;
}

export function useSampleListRegistry() {
  return useInfiniteQuery<PaginatedResponse>({
    queryKey: ["sampleListRegistry"],
    queryFn: async ({ pageParam }) => {
      const cursor = pageParam as number;
      return new Promise<PaginatedResponse>((resolve) => {
        setTimeout(() => {
          const start = cursor;
          const end = start + PAGE_SIZE;
          const paginatedServers = SampleServers.servers.slice(start, end);
          const hasMore = end < SampleServers.servers.length;

          resolve({
            servers: paginatedServers,
            nextCursor: hasMore ? end : null,
          });
        }, 1500);
      });
    },
    initialPageParam: 0,
    getNextPageParam: (lastPage) => lastPage.nextCursor,
  });
}
