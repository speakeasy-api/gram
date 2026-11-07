import type { CatalogServer } from "../hooks";

/** Salesforce's native endpoints and the separate hosted CRM contract. */
const salesforceHostedUrl =
  "https://app.getgram.ai/mcp/speakeasy-team-salesforce";
export const salesforceServers = [
  {
    id: "sobject-reads",
    title: "SObject Reads",
    description: "Find and read records without changing them.",
    productionPath: "platform/sobject-reads",
    sandboxPath: "sandbox/platform/sobject-reads",
    prerequisites: [],
  },
  {
    id: "sobject-mutations",
    title: "SObject Mutations",
    description: "Read, create, and update records without deleting them.",
    productionPath: "platform/sobject-mutations",
    sandboxPath: "sandbox/platform/sobject-mutations",
    prerequisites: [],
  },
  {
    id: "sobject-deletes",
    title: "SObject Deletes",
    description: "Find and delete records without creating or updating them.",
    productionPath: "platform/sobject-deletes",
    sandboxPath: "sandbox/platform/sobject-deletes",
    prerequisites: [],
  },
  {
    id: "sobject-all",
    title: "SObject All",
    description: "Read, create, update, and delete records.",
    productionPath: "platform/sobject-all",
    sandboxPath: "sandbox/platform/sobject-all",
    prerequisites: [],
  },
  {
    id: "data360",
    title: "Data 360",
    description: "Query, transform, and manage Data 360 customer data.",
    productionPath: "data/data360",
    sandboxPath: "data/sandbox/data360",
    prerequisites: [
      "Enable a Data 360 license in this org and use API version 66.0 or later.",
      "The connecting user needs View Data 360 for read-only operations or Manage Data 360 for configuration operations.",
    ],
  },
  {
    id: "headless-360",
    title: "Headless 360 (Beta)",
    description: "Discover and run Salesforce platform and setup operations.",
    productionPath: "platform/headless-360",
    sandboxPath: "sandbox/platform/headless-360",
    prerequisites: [
      "Use API version 67.0 or later. Headless 360 is a Salesforce beta service.",
      "Operations use the connecting user’s existing Salesforce permissions, including permissions to change org configuration.",
    ],
  },
  {
    id: "tableau-next",
    title: "Tableau Next",
    description: "Explore semantic models, dashboards, metrics, and analytics.",
    productionPath: "analytics/tableau-next",
    sandboxPath: "sandbox/analytics/tableau-next",
    prerequisites: [
      "Complete Tableau Next guided or manual setup in this org before connecting its MCP server.",
      "The connecting user needs access to the Tableau Next models and analytics they want to use.",
    ],
  },
] as const;

function nativeUrl(
  server: (typeof salesforceServers)[number],
  environment: string,
): string {
  return `https://api.salesforce.com/platform/mcp/v1/${environment === "sandbox" ? server.sandboxPath : server.productionPath}`;
}

/** The guided page owns Salesforce's endpoint and instance choices. */
export function salesforceSetupInstallServer(
  server: CatalogServer,
): CatalogServer | null {
  const remotes = server.remotes ?? [];
  const remote =
    remotes.find(
      (item) => item.url === nativeUrl(salesforceServers[0], "production"),
    ) ?? remotes.find((item) => salesforceSelectionForUrl(item.url));
  if (!remote) return null;
  return {
    ...server,
    remotes: [
      {
        ...remote,
        headers: remote.headers?.filter(
          (header) => header.name.toLowerCase() !== "mcp-instance-url",
        ),
      },
    ],
  };
}

type Selection = {
  integration: string;
  environment: string;
  salesforceServer: string;
  instanceUrl: string;
  clientSecret?: string;
};
export function salesforceInstance(value: string): string | null {
  try {
    const url = new URL(value.trim());
    if (
      url.protocol !== "https:" ||
      url.port ||
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      !["", "/"].includes(url.pathname) ||
      !url.hostname.endsWith(".salesforce.com")
    )
      return null;
    return url.origin;
  } catch {
    return null;
  }
}
export function salesforceSelectionForUrl(
  value: string | undefined,
): Partial<Selection> | null {
  if (!value) return null;
  try {
    const url = new URL(value);
    if (
      url.protocol !== "https:" ||
      url.port ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    )
      return null;
    const clean = `${url.origin}${url.pathname.replace(/\/+$/, "")}`;
    if (clean === salesforceHostedUrl) return { integration: "hosted" };
    if (url.origin !== "https://api.salesforce.com") return null;
    for (const server of salesforceServers) {
      for (const environment of ["production", "sandbox"]) {
        if (clean === nativeUrl(server, environment))
          return {
            integration: "native",
            environment,
            salesforceServer: server.id,
          };
      }
    }
    return null;
  } catch {
    return null;
  }
}
export function salesforceConnection(values: Selection): {
  name: string;
  issuer: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  url: string;
  instanceUrl: string | null;
  scopes: string[];
  authMethod: "none" | "client_secret_post";
} | null {
  const hosted = values.integration === "hosted";
  if (!hosted && values.integration !== "native") return null;
  const instance = hosted ? salesforceInstance(values.instanceUrl) : null;
  if (hosted && !instance) return null;
  if (!["production", "sandbox"].includes(values.environment)) return null;
  const server = salesforceServers.find(
    (server) => server.id === values.salesforceServer,
  );
  if (!hosted && !server) return null;
  const issuer =
    instance ??
    (values.environment === "sandbox"
      ? "https://test.salesforce.com"
      : "https://login.salesforce.com");
  return {
    name: "Salesforce",
    issuer,
    authorizationEndpoint: `${issuer}/services/oauth2/authorize`,
    tokenEndpoint: `${issuer}/services/oauth2/token`,
    url: hosted ? salesforceHostedUrl : nativeUrl(server!, values.environment),
    instanceUrl: instance,
    scopes: hosted ? ["api", "refresh_token"] : ["mcp_api", "refresh_token"],
    authMethod:
      hosted && values.clientSecret?.trim()
        ? ("client_secret_post" as const)
        : ("none" as const),
  };
}
