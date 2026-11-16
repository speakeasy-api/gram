import type { CatalogServer } from "../hooks";
import type { RemoteMcpServerHeader } from "@gram/client/models/components/remotemcpserverheader";

export function salesforceAppInstallationUrl(
  environment: string,
  domain = "",
): string | undefined {
  const selected = domain.trim()
    ? salesforceMyDomain(domain, environment)
    : null;
  if (domain.trim() && !selected) return undefined;
  const origin =
    selected?.origin ??
    (environment === "sandbox"
      ? "https://test.salesforce.com"
      : "https://login.salesforce.com");
  return `${origin}/packaging/installPackage.apexp?p0=04tdM000000cNGXQA2`;
}

export const salesforceInstanceHeader = "MCP-Instance-URL";
const isSalesforceInstanceHeader = (name: string): boolean =>
  name.toLowerCase() === salesforceInstanceHeader.toLowerCase();

export class SalesforceInstanceConfigurationError extends Error {}

export function salesforceInstanceConfiguration(
  headers: RemoteMcpServerHeader[],
): { header: RemoteMcpServerHeader | undefined; instanceUrl: string } {
  const instances = headers.filter((header) =>
    isSalesforceInstanceHeader(header.name),
  );
  if (
    instances.length > 1 ||
    instances.some((header) => header.isSecret || header.valueFromRequestHeader)
  )
    throw new SalesforceInstanceConfigurationError(
      "In identity settings, keep one plain MCP-Instance-URL header with a fixed Salesforce instance URL; remove duplicates, secret values and request-header mappings.",
    );
  const header = instances[0];
  const raw = header?.value ?? "";
  return { header, instanceUrl: salesforceInstance(raw) ?? raw };
}

export function salesforceScopes(): string[] {
  return ["mcp_api", "refresh_token"];
}

export function salesforceLogin(issuer = "https://login.salesforce.com"): {
  name: string;
  issuer: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
} {
  return {
    name: "Salesforce",
    issuer,
    authorizationEndpoint: `${issuer}/services/oauth2/authorize`,
    tokenEndpoint: `${issuer}/services/oauth2/token`,
  };
}

function strictHttpsUrl(value: string): URL | null {
  try {
    const url = new URL(value);
    return url.protocol !== "https:" ||
      url.port ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
      ? null
      : url;
  } catch {
    return null;
  }
}

/** Normalize known Salesforce page hosts to their matching My Domain login. */
export function salesforceMyDomain(
  value: string,
  environment: string,
): {
  origin: string;
  name: string;
  partition: "" | "sandbox" | "develop";
} | null {
  const input = value.trim();
  let url: URL;
  try {
    url = new URL(input.includes("://") ? input : `https://${input}`);
  } catch {
    return null;
  }
  if (url.protocol !== "https:" || url.port || url.username || url.password)
    return null;
  const hostname = url.hostname
    .replace(/\.my\.salesforce-setup\.com$/, ".my.salesforce.com")
    .replace(/\.lightning\.force\.com$/, ".my.salesforce.com");
  const partition =
    environment === "sandbox"
      ? "sandbox"
      : hostname.endsWith(".develop.my.salesforce.com")
        ? "develop"
        : "";
  const suffix = `.${partition ? `${partition}.` : ""}my.salesforce.com`;
  if (!hostname.endsWith(suffix)) return null;
  const name = hostname.slice(0, -suffix.length);
  if (!/^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$/.test(name)) return null;
  if (
    environment === "sandbox" &&
    !/^[a-z0-9][a-z0-9-]*--[a-z0-9][a-z0-9-]*$/.test(name)
  )
    return null;
  return { origin: `https://${hostname}`, name, partition };
}

/** Native MCP servers documented by the current Salesforce setup guide. */
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
  domainName?: string,
  partition = environment === "sandbox" ? "sandbox" : "",
): string {
  if (domainName)
    return `https://api.salesforce.com/platform/mcp/v1/d/${domainName}/${partition ? `${partition}/` : ""}${server.productionPath}`;
  return `https://api.salesforce.com/platform/mcp/v1/${environment === "sandbox" ? server.sandboxPath : server.productionPath}`;
}

/** The guided page owns Salesforce's endpoint and instance choices. */
export function salesforceSetupInstallServer(
  server: CatalogServer,
): CatalogServer | null {
  const remotes = server.remotes ?? [];
  const reads = salesforceServers.find((item) => item.id === "sobject-reads");
  const remote =
    remotes.find(
      (item) => reads && item.url === nativeUrl(reads, "production"),
    ) ?? remotes.find((item) => salesforceSelectionForUrl(item.url));
  if (!remote) return null;
  return {
    ...server,
    remotes: [
      {
        ...remote,
        headers: remote.headers?.filter(
          (header) => !isSalesforceInstanceHeader(header.name),
        ),
      },
    ],
  };
}

type Selection = {
  integration: string;
  environment: string;
  salesforceServer: string;
  salesforceDomain?: string;
  instanceUrl: string;
  clientSecret?: string;
  path?: string;
};
export function salesforceInstance(value: string): string | null {
  const url = strictHttpsUrl(value.trim());
  if (
    !url ||
    !["", "/"].includes(url.pathname) ||
    !url.hostname.endsWith(".salesforce.com")
  )
    return null;
  return url.origin;
}

export function salesforceSelectionForUrl(
  value: string | undefined,
): Partial<Selection> | null {
  if (!value) return null;
  const url = strictHttpsUrl(value);
  if (!url) return null;
  const clean = `${url.origin}${url.pathname.replace(/\/+$/, "")}`;
  if (url.origin !== "https://api.salesforce.com") return null;
  const scoped =
    /^\/platform\/mcp\/v1\/d\/([^/]+)\/(?:(sandbox|develop)\/)?/.exec(
      url.pathname,
    );
  for (const server of salesforceServers) {
    for (const environment of ["production", "sandbox"]) {
      if (clean === nativeUrl(server, environment))
        return {
          integration: "native",
          environment,
          salesforceServer: server.id,
        };
      if (scoped) {
        const domain = salesforceMyDomain(
          `https://${scoped[1]}${scoped[2] ? `.${scoped[2]}` : ""}.my.salesforce.com`,
          environment,
        );
        if (
          domain &&
          clean ===
            nativeUrl(server, environment, domain.name, domain.partition)
        )
          return {
            integration: "native",
            environment,
            salesforceServer: server.id,
            salesforceDomain: domain.origin,
          };
      }
    }
  }
  return null;
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
  if (values.integration !== "native") return null;
  if (!["production", "sandbox"].includes(values.environment)) return null;
  const server = salesforceServers.find(
    (server) => server.id === values.salesforceServer,
  );
  if (!server) return null;
  const domain = values.salesforceDomain?.trim()
    ? salesforceMyDomain(values.salesforceDomain, values.environment)
    : null;
  if (values.salesforceDomain?.trim() && !domain) return null;
  const issuer =
    domain?.origin ??
    (values.environment === "sandbox"
      ? "https://test.salesforce.com"
      : "https://login.salesforce.com");
  return {
    ...salesforceLogin(issuer),
    url: nativeUrl(server, values.environment, domain?.name, domain?.partition),
    instanceUrl: null,
    scopes: salesforceScopes(),
    authMethod: "none",
  };
}
