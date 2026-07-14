import type { PulseMCPServer } from "@/pages/catalog/hooks";
import type { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";
import type { ExternalMCPRemoteHeader } from "@gram/client/models/components/externalmcpremoteheader.js";

export function filterToHttpRemotes(server: PulseMCPServer): PulseMCPServer {
  const httpRemotes = server.remotes?.filter(
    (r) => r.transportType === "streamable-http",
  );
  return {
    ...server,
    remotes: httpRemotes ? dedupeRemotesByUrl(httpRemotes) : httpRemotes,
  };
}

// Some registry entries publish multiple remotes with the same URL that differ
// only by their `headers` array (e.g. one variant for OAuth, another for
// static API-key auth). Only one remote MCP server is created per URL, so the
// second variant is unreachable — collapse the duplicate so users do not see
// two identical-looking checkboxes.
export function dedupeRemotesByUrl(
  remotes: ExternalMCPRemote[],
): ExternalMCPRemote[] {
  const byUrl = new Map<string, ExternalMCPRemote>();
  for (const r of remotes) {
    if (!byUrl.has(r.url)) byUrl.set(r.url, r);
  }
  return Array.from(byUrl.values());
}

// Remote MCP server URLs are stored canonically without trailing slashes;
// normalize catalog URLs the same way before comparing.
export function normalizeRemoteUrl(url: string): string {
  return url.replace(/\/+$/g, "");
}

// Headers worth collecting from the user when installing a catalog remote.
// The Authorization header is never collected: on DCR-capable servers auth
// auto-configuration owns it, and everywhere else auth is better set up from
// the server's Settings tab than by pasting a static bearer value here.
export function collectibleHeaders(
  remote: ExternalMCPRemote,
): ExternalMCPRemoteHeader[] {
  return (remote.headers ?? []).filter(
    (header) => header.name.toLowerCase() !== "authorization",
  );
}

// Header requirements the catalog publishes for a given remote endpoint URL.
// Used to suggest header rows for remote MCP servers that were created from
// (or happen to match) a catalog entry. Registries may list the same URL under
// several servers or remote variants; the first variant that actually carries
// collectible headers wins.
export function catalogHeadersForRemoteUrl(
  servers: PulseMCPServer[],
  url: string,
): ExternalMCPRemoteHeader[] {
  const normalized = normalizeRemoteUrl(url);
  for (const server of servers) {
    for (const remote of server.remotes ?? []) {
      if (normalizeRemoteUrl(remote.url) !== normalized) continue;
      const headers = collectibleHeaders(remote);
      if (headers.length > 0) return headers;
    }
  }
  return [];
}

/** Friendly display names and descriptions for known remote endpoints */
const REMOTE_DISPLAY_INFO: Record<
  string,
  { name: string; description: string }
> = {
  // Salesforce Industry Clouds
  "insurance-cloud": {
    name: "Insurance Cloud",
    description: "Policy management, claims processing, and underwriting",
  },
  "health-cloud": {
    name: "Health Cloud",
    description: "Patient care coordination and healthcare management",
  },
  "consumer-goods-cloud": {
    name: "Consumer Goods Cloud",
    description: "Retail execution, trade promotion, and field operations",
  },
  "manufacturing-cloud": {
    name: "Manufacturing Cloud",
    description: "Sales agreements, account forecasting, and production",
  },
  "automotive-cloud": {
    name: "Automotive Cloud",
    description: "Vehicle sales, service, and driver engagement",
  },
  "communications-cloud": {
    name: "Communications Cloud",
    description: "Order management and telecom service configuration",
  },
  "media-cloud": {
    name: "Media Cloud",
    description: "Ad sales, content distribution, and subscriber management",
  },
  "financial-services-cloud": {
    name: "Financial Services Cloud",
    description: "Wealth management, banking, and financial planning",
  },
  "nonprofit-cloud": {
    name: "Nonprofit Cloud",
    description: "Fundraising, grants, and program management",
  },
  "education-cloud": {
    name: "Education Cloud",
    description: "Student lifecycle, admissions, and learning management",
  },
  "public-sector": {
    name: "Public Sector",
    description: "Government services, permits, and case management",
  },
  "energy-utilities-cloud": {
    name: "Energy & Utilities Cloud",
    description: "Meter data, field service, and customer programs",
  },
  "loyalty-management": {
    name: "Loyalty Management",
    description: "Points, rewards, and member engagement programs",
  },
  "pricing-ngp": {
    name: "Industries Pricing",
    description: "Dynamic pricing, quotes, and product configuration",
  },
  "rebate-management": {
    name: "Rebate Management",
    description: "Rebate programs, calculations, and payouts",
  },
  "document-generation": {
    name: "Document Generation",
    description: "Automated document creation and templates",
  },
  omnistudio: {
    name: "OmniStudio",
    description: "Guided flows, data integration, and UI components",
  },
  core: {
    name: "Salesforce Core",
    description: "Standard CRM objects and platform features",
  },
  // Salesforce Platform APIs
  "sobject-all": {
    name: "SObject All",
    description: "Full CRUD access to all Salesforce objects",
  },
  "sobject-reads": {
    name: "SObject Reads",
    description: "Read-only access to Salesforce objects",
  },
  "sobject-mutations": {
    name: "SObject Mutations",
    description: "Create and update Salesforce records",
  },
  "sobject-deletes": {
    name: "SObject Deletes",
    description: "Delete Salesforce records",
  },
  "invocable-actions": {
    name: "Invocable Actions",
    description: "Execute Flows, Apex actions, and quick actions",
  },
  invocable_actions: {
    name: "Invocable Actions",
    description: "Execute Flows, Apex actions, and quick actions",
  },
  "salesforce-api-context": {
    name: "API Context",
    description: "Org info, user details, and API limits",
  },
  "data-cloud-queries": {
    name: "Data Cloud Queries",
    description: "Query unified customer profiles and segments",
  },
  "tableau-next": {
    name: "Tableau Next",
    description: "Analytics, dashboards, and data visualization",
  },
  "revenue-cloud": {
    name: "Revenue Cloud",
    description: "CPQ, billing, and subscription management",
  },
};

/** Get friendly display info for a remote URL */
export function getRemoteDisplayInfo(url: string): {
  name: string;
  description: string;
} {
  try {
    const parsedUrl = new URL(url);
    const pathParts = parsedUrl.pathname.split("/").filter(Boolean);
    const endpoint = pathParts[pathParts.length - 1] || "";

    // Check for known endpoints
    const info = REMOTE_DISPLAY_INFO[endpoint.toLowerCase()];
    if (info) return info;

    // Fallback: format the endpoint name nicely
    const formattedName = endpoint
      .split("-")
      .map((word) => word.charAt(0).toUpperCase() + word.slice(1))
      .join(" ");

    return {
      name: formattedName || endpoint,
      description: parsedUrl.host,
    };
  } catch {
    return { name: url, description: "" };
  }
}
