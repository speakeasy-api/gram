import type { QueryClient } from "@tanstack/react-query";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients";
import { invalidateAllRemoteSessionClient } from "@gram/client/react-query/remoteSessionClient";
import { invalidateAllServerClientAssociations } from "@gram/client/react-query/serverClientAssociations";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers";
import { invalidateAllRemoteSessionIssuer } from "@gram/client/react-query/remoteSessionIssuer";
import { invalidateAllRemoteSessionsCount } from "@gram/client/react-query/remoteSessionsCount";
import { invalidateAllGetRemoteMcpServerScopes } from "@gram/client/react-query/getRemoteMcpServerScopes";
import { invalidateSharedIssuerImpact } from "../model/upstreamRepointing";

/** Refresh all views of a saved upstream identity configuration. */
export async function invalidateIdentityConfiguration(
  queryClient: QueryClient,
): Promise<void> {
  await Promise.all([
    invalidateAllRemoteSessionClients(queryClient),
    invalidateAllRemoteSessionClient(queryClient),
    invalidateAllServerClientAssociations(queryClient),
    invalidateAllRemoteSessionIssuers(queryClient),
    invalidateAllRemoteSessionIssuer(queryClient),
    invalidateAllRemoteSessionsCount(queryClient),
    invalidateAllGetRemoteMcpServerScopes(queryClient),
    invalidateSharedIssuerImpact(queryClient),
  ]);
}
