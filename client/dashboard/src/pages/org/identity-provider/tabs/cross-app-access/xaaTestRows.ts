import type { XaaServerReadiness } from "@gram/client/models/components/xaaserverreadiness.js";

export function pendingRow(index: number): XaaServerReadiness {
  return {
    mcpServerId: `server-${index}`,
    serverName: `Server ${index}`,
    serverSlug: `server-${index}`,
    projectId: "project",
    projectSlug: "project",
    resourceIndicator: "https://resource.example.com",
    issuerId: "00000000-0000-4000-8000-000000000001",
    scopes: [],
    clientBinding: "single",
    state: "needs_connection",
    pending: true,
  };
}

export function confirmedRow(
  index = 0,
  overrides: Partial<XaaServerReadiness> = {},
): XaaServerReadiness {
  return {
    ...pendingRow(index),
    state: "connected",
    pending: false,
    audience: "https://issuer.example.com/saved",
    oktaApplicationId: "recorded-app",
    ...overrides,
  };
}
