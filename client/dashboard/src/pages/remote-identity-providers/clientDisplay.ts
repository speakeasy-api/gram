import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";

// remoteSessionClientDisplayName is the short label for a remote_session_client
// in breadcrumbs, lists, and selectors. A CIMD client's client_id is the hosted
// metadata-document URL, which is too long to read inline; fall back to the row
// id (which is also the trailing path segment of that URL). Manual/DCR clients
// keep their real, human-sized client_id. Detail surfaces that document the
// actual client_id (e.g. the Overview "Client ID" field) keep the full value.
export function remoteSessionClientDisplayName(
  client: Pick<RemoteSessionClient, "id" | "clientId" | "clientIdMetadataUri">,
): string {
  if (client.clientIdMetadataUri) return client.id;
  return client.clientId;
}
