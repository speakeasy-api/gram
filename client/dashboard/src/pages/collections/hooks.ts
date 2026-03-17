import { useMemo, useState } from "react";
import type { Collection } from "./types";

const MOCK_COLLECTIONS: Collection[] = [
  {
    id: "col_1",
    name: "Developer Productivity Suite",
    description:
      "Essential tools for developers — GitHub, Linear, and Sentry integrations for issue tracking, code management, and error monitoring.",
    visibility: "public",
    servers: [
      {
        registrySpecifier: "io.github.github/github-mcp-server",
        title: "GitHub",
        description: "Access repositories, issues, and pull requests",
        iconUrl: "https://github.githubassets.com/favicons/favicon.svg",
        toolCount: 12,
      },
      {
        registrySpecifier: "app.linear/linear",
        title: "Linear",
        description: "Manage issues, projects, and teams",
        iconUrl: "https://linear.app/favicon.ico",
        toolCount: 8,
      },
      {
        registrySpecifier: "io.github.getsentry/sentry-mcp",
        title: "Sentry",
        description: "Monitor errors and performance",
        iconUrl: undefined,
        toolCount: 6,
      },
    ],
    author: { orgName: "Gram", orgId: "org_gram" },
    installCount: 1842,
    createdAt: "2025-11-01T00:00:00Z",
    updatedAt: "2026-02-15T00:00:00Z",
  },
  {
    id: "col_2",
    name: "Design & Collaboration",
    description:
      "Bring design tools into your AI workflow. Figma for design systems and Notion for documentation and knowledge management.",
    visibility: "public",
    servers: [
      {
        registrySpecifier: "com.figma.mcp/mcp",
        title: "Figma",
        description: "Access design files, components, and styles",
        iconUrl: "https://static.figma.com/app/icon/1/favicon.svg",
        toolCount: 10,
      },
      {
        registrySpecifier: "com.notion/mcp",
        title: "Notion",
        description: "Read and manage pages, databases, and content",
        iconUrl: "https://www.notion.so/favicon.ico",
        toolCount: 9,
      },
    ],
    author: { orgName: "Gram", orgId: "org_gram" },
    installCount: 956,
    createdAt: "2025-12-10T00:00:00Z",
    updatedAt: "2026-01-20T00:00:00Z",
  },
  {
    id: "col_3",
    name: "Data & Analytics",
    description:
      "Connect your data sources for AI-powered analytics. Query databases, explore dashboards, and generate insights.",
    visibility: "public",
    servers: [
      {
        registrySpecifier: "com.snowflake/mcp-server",
        title: "Snowflake",
        description: "Query data warehouses and manage tables",
        toolCount: 7,
      },
      {
        registrySpecifier: "io.github.datadog/mcp-server",
        title: "Datadog",
        description: "Monitor metrics, logs, and traces",
        toolCount: 11,
      },
    ],
    author: { orgName: "Gram", orgId: "org_gram" },
    installCount: 621,
    createdAt: "2026-01-05T00:00:00Z",
    updatedAt: "2026-03-01T00:00:00Z",
  },
  {
    id: "col_4",
    name: "Customer Support Stack",
    description:
      "End-to-end customer support automation. Manage tickets, track conversations, and surface knowledge base articles.",
    visibility: "public",
    servers: [
      {
        registrySpecifier: "com.zendesk/mcp-server",
        title: "Zendesk",
        description: "Manage support tickets and customer data",
        toolCount: 9,
      },
      {
        registrySpecifier: "com.intercom/mcp-server",
        title: "Intercom",
        description: "Access conversations and user profiles",
        toolCount: 7,
      },
      {
        registrySpecifier: "io.github.confluence/mcp-server",
        title: "Confluence",
        description: "Search and manage knowledge base articles",
        toolCount: 5,
      },
    ],
    author: { orgName: "Acme Corp", orgId: "org_acme" },
    installCount: 334,
    createdAt: "2026-01-15T00:00:00Z",
    updatedAt: "2026-02-28T00:00:00Z",
  },
  {
    id: "col_5",
    name: "Internal Tools",
    description:
      "Private collection for our engineering team. Custom MCP servers for internal APIs and automation workflows.",
    visibility: "private",
    servers: [
      {
        registrySpecifier: "internal/deploy-bot",
        title: "Deploy Bot",
        description: "Trigger and monitor deployments",
        toolCount: 4,
      },
      {
        registrySpecifier: "internal/feature-flags",
        title: "Feature Flags",
        description: "Manage feature flag configurations",
        toolCount: 3,
      },
    ],
    author: { orgName: "My Org", orgId: "org_current" },
    installCount: 12,
    createdAt: "2026-02-01T00:00:00Z",
    updatedAt: "2026-03-10T00:00:00Z",
  },
  {
    id: "col_6",
    name: "Sales & CRM",
    description:
      "Supercharge your sales workflow with CRM access, email outreach, and pipeline management tools.",
    visibility: "public",
    servers: [
      {
        registrySpecifier: "com.salesforce/mcp-server",
        title: "Salesforce",
        description: "Manage leads, opportunities, and accounts",
        toolCount: 14,
      },
      {
        registrySpecifier: "com.hubspot/mcp-server",
        title: "HubSpot",
        description: "Access contacts, deals, and marketing data",
        toolCount: 11,
      },
    ],
    author: { orgName: "Gram", orgId: "org_gram" },
    installCount: 478,
    createdAt: "2026-01-20T00:00:00Z",
    updatedAt: "2026-03-05T00:00:00Z",
  },
  {
    id: "col_7",
    name: "DevOps Essentials",
    description:
      "Infrastructure and deployment tooling. Monitor services, manage containers, and automate CI/CD pipelines.",
    visibility: "public",
    servers: [
      {
        registrySpecifier: "io.github.kubernetes/mcp-server",
        title: "Kubernetes",
        description: "Manage clusters, pods, and deployments",
        toolCount: 15,
      },
      {
        registrySpecifier: "com.pagerduty/mcp-server",
        title: "PagerDuty",
        description: "Manage incidents and on-call schedules",
        toolCount: 6,
      },
      {
        registrySpecifier: "io.github.terraform/mcp-server",
        title: "Terraform",
        description: "Plan and apply infrastructure changes",
        toolCount: 5,
      },
    ],
    author: { orgName: "Acme Corp", orgId: "org_acme" },
    installCount: 892,
    createdAt: "2025-11-20T00:00:00Z",
    updatedAt: "2026-02-20T00:00:00Z",
  },
  {
    id: "col_8",
    name: "Shared QA Collection",
    description:
      "QA automation tools shared across our partner organizations. Includes testing frameworks and bug tracking.",
    visibility: "private",
    sharedWithOrgIds: ["org_partner1", "org_partner2"],
    servers: [
      {
        registrySpecifier: "internal/test-runner",
        title: "Test Runner",
        description: "Execute and manage test suites",
        toolCount: 6,
      },
      {
        registrySpecifier: "internal/bug-tracker",
        title: "Bug Tracker",
        description: "Track and triage reported bugs",
        toolCount: 4,
      },
    ],
    author: { orgName: "My Org", orgId: "org_current" },
    installCount: 28,
    createdAt: "2026-02-10T00:00:00Z",
    updatedAt: "2026-03-12T00:00:00Z",
  },
];

const CURRENT_ORG_ID = "org_current";

export function useCollections(
  tab: "discover" | "org",
  search?: string,
): {
  data: Collection[];
  isLoading: boolean;
} {
  const data = useMemo(() => {
    let filtered: Collection[];

    if (tab === "discover") {
      filtered = MOCK_COLLECTIONS.filter(
        (c) =>
          c.visibility === "public" ||
          c.sharedWithOrgIds?.includes(CURRENT_ORG_ID),
      );
    } else {
      filtered = MOCK_COLLECTIONS.filter(
        (c) => c.author.orgId === CURRENT_ORG_ID,
      );
    }

    if (search) {
      const q = search.toLowerCase();
      filtered = filtered.filter(
        (c) =>
          c.name.toLowerCase().includes(q) ||
          c.description.toLowerCase().includes(q) ||
          c.servers.some((s) => s.title.toLowerCase().includes(q)),
      );
    }

    return filtered;
  }, [tab, search]);

  return { data, isLoading: false };
}

export function useCollectionDetail(id: string): {
  data: Collection | null;
  isLoading: boolean;
} {
  const data = useMemo(() => {
    return MOCK_COLLECTIONS.find((c) => c.id === id) ?? null;
  }, [id]);

  return { data, isLoading: false };
}

export function useCreateCollection(): {
  mutate: (
    collection: Omit<
      Collection,
      "id" | "createdAt" | "updatedAt" | "installCount"
    >,
  ) => void;
  isPending: boolean;
} {
  const [isPending, setIsPending] = useState(false);

  const mutate = (
    _collection: Omit<
      Collection,
      "id" | "createdAt" | "updatedAt" | "installCount"
    >,
  ) => {
    setIsPending(true);
    setTimeout(() => {
      setIsPending(false);
    }, 1000);
  };

  return { mutate, isPending };
}

/**
 * Mock catalog servers for the server picker on the create page.
 * Replaces useInfiniteListMCPCatalog which requires project-scoped auth.
 */
const MOCK_CATALOG_SERVERS = [
  {
    registrySpecifier: "io.github.github/github-mcp-server",
    title: "GitHub",
    description: "Access repositories, issues, and pull requests",
    iconUrl: "https://github.githubassets.com/favicons/favicon.svg",
    toolCount: 12,
  },
  {
    registrySpecifier: "app.linear/linear",
    title: "Linear",
    description: "Manage issues, projects, and teams",
    iconUrl: "https://linear.app/favicon.ico",
    toolCount: 8,
  },
  {
    registrySpecifier: "com.figma.mcp/mcp",
    title: "Figma",
    description: "Access design files, components, and styles",
    iconUrl: "https://static.figma.com/app/icon/1/favicon.svg",
    toolCount: 10,
  },
  {
    registrySpecifier: "com.notion/mcp",
    title: "Notion",
    description: "Read and manage pages, databases, and content",
    iconUrl: "https://www.notion.so/favicon.ico",
    toolCount: 9,
  },
  {
    registrySpecifier: "com.snowflake/mcp-server",
    title: "Snowflake",
    description: "Query data warehouses and manage tables",
    toolCount: 7,
  },
  {
    registrySpecifier: "io.github.datadog/mcp-server",
    title: "Datadog",
    description: "Monitor metrics, logs, and traces",
    toolCount: 11,
  },
  {
    registrySpecifier: "com.salesforce/mcp-server",
    title: "Salesforce",
    description: "Manage leads, opportunities, and accounts",
    toolCount: 14,
  },
  {
    registrySpecifier: "com.hubspot/mcp-server",
    title: "HubSpot",
    description: "Access contacts, deals, and marketing data",
    toolCount: 11,
  },
  {
    registrySpecifier: "io.github.kubernetes/mcp-server",
    title: "Kubernetes",
    description: "Manage clusters, pods, and deployments",
    toolCount: 15,
  },
  {
    registrySpecifier: "com.pagerduty/mcp-server",
    title: "PagerDuty",
    description: "Manage incidents and on-call schedules",
    toolCount: 6,
  },
  {
    registrySpecifier: "com.zendesk/mcp-server",
    title: "Zendesk",
    description: "Manage support tickets and customer data",
    toolCount: 9,
  },
  {
    registrySpecifier: "io.github.terraform/mcp-server",
    title: "Terraform",
    description: "Plan and apply infrastructure changes",
    toolCount: 5,
  },
];

export interface CatalogServer {
  registrySpecifier: string;
  title: string;
  description: string;
  iconUrl?: string;
  toolCount: number;
}

export function useCatalogServers(search?: string): {
  data: CatalogServer[];
  isLoading: boolean;
} {
  const data = useMemo(() => {
    if (!search) return MOCK_CATALOG_SERVERS;
    const q = search.toLowerCase();
    return MOCK_CATALOG_SERVERS.filter(
      (s) =>
        s.title.toLowerCase().includes(q) ||
        s.description.toLowerCase().includes(q),
    );
  }, [search]);

  return { data, isLoading: false };
}

export function useInstallCollection(): {
  mutate: (params: { collectionId: string; projectId: string }) => void;
  isPending: boolean;
  isSuccess: boolean;
  reset: () => void;
} {
  const [isPending, setIsPending] = useState(false);
  const [isSuccess, setIsSuccess] = useState(false);

  const mutate = (_params: { collectionId: string; projectId: string }) => {
    setIsPending(true);
    setIsSuccess(false);
    setTimeout(() => {
      setIsPending(false);
      setIsSuccess(true);
    }, 1500);
  };

  const reset = () => {
    setIsSuccess(false);
    setIsPending(false);
  };

  return { mutate, isPending, isSuccess, reset };
}
