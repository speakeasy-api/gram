import type { UserSessionClient } from "@gram/client/models/components/usersessionclient.js";
import { useRevokeUserSessionClientMutation } from "@gram/client/react-query/revokeUserSessionClient.js";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import {
  clientDocumentOrigin,
  userSessionClientSource,
} from "@/lib/user-session-client-source";

export function RevokeClientDialog({
  client,
  open,
  onOpenChange,
  onRevoked,
}: {
  client: UserSessionClient;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onRevoked: () => void;
}): JSX.Element {
  const revoke = useRevokeUserSessionClientMutation();
  const isCimd = userSessionClientSource(client) === "cimd";
  const origin = clientDocumentOrigin(client);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Revoke agent?</Dialog.Title>
          <Dialog.Description>
            This revokes {client.clientName}
            {origin ? ` (${origin})` : ""} and ends every session it established
            through this issuer, including sessions on any other MCP server the
            issuer gates. Anyone using it will need to authenticate again.
          </Dialog.Description>
        </Dialog.Header>
        {isCimd && (
          // A CIMD client's identity is its metadata document URL, and the
          // partial unique index means a revoked row stops conflicting — so
          // the next /authorize re-resolves the document and registers a fresh
          // row. Revoking clears today's access; it does not block the client.
          // Durable blocking is admission control's job.
          <Alert variant="warning">
            <AlertTitle>Revoking will not block this agent</AlertTitle>
            <AlertDescription>
              This agent is identified by its metadata document
              {origin ? ` at ${origin}` : ""}, not by a registration Gram
              issued. It can register again the next time it authorizes.
              Revoking ends its current sessions, but it does not lock the agent
              out.
            </AlertDescription>
          </Alert>
        )}
        <Dialog.Footer>
          <Button variant="tertiary" onClick={() => onOpenChange(false)}>
            Cancel
          </Button>
          <Button
            variant="destructive-primary"
            disabled={revoke.isPending}
            onClick={() =>
              revoke.mutate(
                { request: { id: client.id } },
                {
                  onSuccess: () => {
                    onOpenChange(false);
                    onRevoked();
                  },
                },
              )
            }
          >
            {revoke.isPending ? "Revoking…" : "Revoke"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
