/** Salesforce's native SObject endpoints and the separate hosted CRM contract. */
const salesforceHostedUrl =
  "https://app.getgram.ai/mcp/speakeasy-team-salesforce";
export const salesforceServers = [
  {
    id: "sobject-reads",
    title: "SObject Reads",
    description: "Find and read records without changing them.",
  },
  {
    id: "sobject-mutations",
    title: "SObject Mutations",
    description: "Read, create, and update records without deleting them.",
  },
  {
    id: "sobject-deletes",
    title: "SObject Deletes",
    description: "Find and delete records without creating or updating them.",
  },
  {
    id: "sobject-all",
    title: "SObject All",
    description: "Read, create, update, and delete records.",
  },
] as const;

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
    const match = url.pathname
      .replace(/\/+$/, "")
      .match(
        /^\/platform\/mcp\/v1\/(sandbox\/)?platform\/(sobject-reads|sobject-mutations|sobject-deletes|sobject-all)$/,
      );
    return match
      ? {
          integration: "native",
          environment: match[1] ? "sandbox" : "production",
          salesforceServer: match[2]!,
        }
      : null;
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
  if (
    !hosted &&
    !salesforceServers.some((server) => server.id === values.salesforceServer)
  )
    return null;
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
    url: hosted
      ? salesforceHostedUrl
      : `https://api.salesforce.com/platform/mcp/v1/${values.environment === "sandbox" ? "sandbox/" : ""}platform/${values.salesforceServer}`,
    instanceUrl: instance,
    scopes: hosted ? ["api", "refresh_token"] : ["mcp_api", "refresh_token"],
    authMethod:
      hosted && values.clientSecret?.trim()
        ? ("client_secret_post" as const)
        : ("none" as const),
  };
}
