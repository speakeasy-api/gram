import { invalidateAllOrganizationUserSessionIssuerCimdClients } from "@gram/client/react-query/organizationUserSessionIssuerCimdClients.js";
import { invalidateAllOrganizationUserSessionIssuer } from "@gram/client/react-query/organizationUserSessionIssuer.js";
import { invalidateAllOrganizationUserSessionIssuerMcpServers } from "@gram/client/react-query/organizationUserSessionIssuerMcpServers.js";
import { invalidateAllOrganizationUserSessionIssuers } from "@gram/client/react-query/organizationUserSessionIssuers.js";
import { invalidateAllUserSessionIssuer } from "@gram/client/react-query/userSessionIssuer.js";
import { invalidateAllUserSessionIssuerCimdClients } from "@gram/client/react-query/userSessionIssuerCimdClients.js";
import { invalidateAllUserSessionIssuers } from "@gram/client/react-query/userSessionIssuers.js";
import type { QueryClient } from "@tanstack/react-query";

/**
 * Refreshes every view of user session issuers after one moves or is
 * consolidated: this page's lists and details, the servers attached to each,
 * and the project-level reads behind every MCP server's issuer picker.
 */
export async function invalidateIssuers(
  queryClient: QueryClient,
): Promise<void> {
  const options = { refetchType: "all" } as const;
  await Promise.all([
    invalidateAllOrganizationUserSessionIssuers(queryClient, options),
    invalidateAllOrganizationUserSessionIssuer(queryClient, options),
    invalidateAllOrganizationUserSessionIssuerMcpServers(queryClient, options),
    invalidateAllUserSessionIssuers(queryClient, options),
    invalidateAllUserSessionIssuer(queryClient, options),
    // Consolidating moves the source's allowed client documents.
    invalidateAllOrganizationUserSessionIssuerCimdClients(queryClient, options),
    invalidateAllUserSessionIssuerCimdClients(queryClient, options),
  ]);
}
