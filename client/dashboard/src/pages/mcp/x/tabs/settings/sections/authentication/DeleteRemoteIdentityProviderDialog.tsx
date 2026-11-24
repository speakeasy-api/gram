import { invalidateAllMcpServerAuthentication } from "@gram/client/react-query/mcpServerAuthentication.js";
import { invalidateAllServerClientAssociations } from "@gram/client/react-query/serverClientAssociations.js";
import { Spinner } from "@/components/ui/Spinner";
import { Input } from "@/components/ui/Input";
import { ServerUsageImpactById } from "@/components/mcp-server-usage/ServerUsageConfirmation";
import { Text } from "@/components/ui/Text";
import { useSdkClient } from "@/contexts/Sdk";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { invalidateAllGetMcpServer } from "@gram/client/react-query/getMcpServer.js";
import { invalidateAllMcpServers } from "@gram/client/react-query/mcpServers.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";

export function DeleteRemoteIdentityProviderDialog({
  open,
  onOpenChange,
  registrationId,
  clientId,
  issuer,
  mcpServerId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** The exact registration to unlink; the provider and registration remain. */
  registrationId: string;
  clientId: string;
  issuer: RemoteSessionIssuer;
  /** The MCP server the provider is removed from, for the usage preview. */
  mcpServerId: string;
}): JSX.Element {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content className="max-w-2xl!">
        {open && (
          <DeleteRemoteIdentityProviderDialogBody
            registrationId={registrationId}
            clientId={clientId}
            issuer={issuer}
            mcpServerId={mcpServerId}
            onClose={() => onOpenChange(false)}
          />
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function DeleteRemoteIdentityProviderDialogBody({
  registrationId,
  clientId,
  issuer,
  mcpServerId,
  onClose,
}: {
  registrationId: string;
  clientId: string;
  issuer: RemoteSessionIssuer;
  mcpServerId: string;
  onClose: () => void;
}) {
  const client = useSdkClient();
  const queryClient = useQueryClient();

  const [confirmation, setConfirmation] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    setConfirmation("");
    setSubmitting(false);
    setError(null);
  }, [issuer.id]);

  const inputMatches = confirmation.trim() === issuer.issuer;

  const handleConfirm = async () => {
    if (!inputMatches || submitting) return;
    setSubmitting(true);
    setError(null);
    try {
      await client.remoteSessions.detachServerClientAssociation({
        serverClientAssociationForm: {
          mcpServerId,
          remoteSessionClientId: registrationId,
        },
      });
      await Promise.all([
        invalidateAllServerClientAssociations(queryClient),
        invalidateAllMcpServerAuthentication(queryClient),
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionIssuers(queryClient, { refetchType: "all" }),
        // Also invalidate MCP server queries so the sidebar readiness bar
        // refreshes (AGE-3279).
        invalidateAllGetMcpServer(queryClient, { refetchType: "all" }),
        invalidateAllMcpServers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Registration unlinked from this server");
      onClose();
    } catch (err) {
      console.error("Delete authorization server failed", err);
      setError(
        err instanceof Error && err.message
          ? err.message
          : "An unexpected error occurred. Please try again.",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <Dialog.Header>
        <Dialog.Title>Unlink Registration</Dialog.Title>
        <Dialog.Description>
          This unlinks the registration <strong>{clientId}</strong> (
          {registrationId}) from this MCP server only. The registration,
          authorization server, and any other associations stay in place.
        </Dialog.Description>
      </Dialog.Header>

      {mcpServerId && (
        <div className="grid gap-2">
          <ServerUsageImpactById mcpServerId={mcpServerId} />
          <Text small muted>
            Counts cover every sign-in to this server, including through its
            other authorization servers.
          </Text>
        </div>
      )}

      <div className="grid gap-2">
        <Text small>
          To confirm, type the issuer URL: <strong>{issuer.issuer}</strong>
        </Text>
        <Input
          value={confirmation}
          onChange={setConfirmation}
          placeholder={issuer.issuer}
          disabled={submitting}
        />
      </div>

      <Alert variant="warning" dismissible={false}>
        Users who signed in through this registration may need to authenticate
        again. If this hosted server requires upstream authentication,
        connecting will stop working until an administrator assigns a
        replacement or explicitly selects No upstream authentication.
      </Alert>

      {error && (
        <Alert variant="error" dismissible={false}>
          {error}
        </Alert>
      )}

      <Dialog.Footer>
        <Button variant="secondary" onClick={onClose} disabled={submitting}>
          <Button.Text>Cancel</Button.Text>
        </Button>
        <Button
          variant="destructive-primary"
          disabled={!inputMatches || submitting}
          onClick={() => void handleConfirm()}
        >
          {submitting && (
            <Button.LeftIcon>
              <Spinner className="mr-0 size-4" />
            </Button.LeftIcon>
          )}
          <Button.Text>{submitting ? "Unlinking" : "Unlink"}</Button.Text>
        </Button>
      </Dialog.Footer>
    </>
  );
}
