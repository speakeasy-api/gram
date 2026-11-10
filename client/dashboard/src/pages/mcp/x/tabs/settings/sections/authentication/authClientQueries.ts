import { invalidateAllAuthClient } from "@gram/client/react-query/authClient.js";
import { invalidateAllAuthClientAudience } from "@gram/client/react-query/authClientAudience.js";
import { invalidateAllAuthClients } from "@gram/client/react-query/authClients.js";
import { invalidateAllMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import type { QueryClient } from "@tanstack/react-query";

/** Every read a change to a credential can make stale. */
export async function invalidateAuthClientQueries(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllMcpServerAuthentication(queryClient),
    invalidateAllAuthClients(queryClient),
    invalidateAllAuthClient(queryClient),
    invalidateAllAuthClientAudience(queryClient),
  ]);
}
