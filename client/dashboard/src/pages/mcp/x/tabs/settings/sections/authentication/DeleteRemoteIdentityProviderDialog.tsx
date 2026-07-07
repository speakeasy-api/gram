import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { useSdkClient } from "@/contexts/Sdk";
import type { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";
import { invalidateAllRemoteSessionClients } from "@gram/client/react-query/remoteSessionClients.js";
import { invalidateAllRemoteSessionIssuers } from "@gram/client/react-query/remoteSessionIssuers.js";
import { Alert, Button, Dialog } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { Loader2 } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

export function DeleteRemoteIdentityProviderDialog({
  open,
  onOpenChange,
  userSessionIssuerId,
  issuer,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // The user_session_issuer this provider is associated with on the current
  // MCP server. We scope the delete to the clients linking this pair —
  // delete-semantics #1: remove the association, leave the
  // remote_session_issuer record alone so other servers can still use it.
  userSessionIssuerId: string;
  issuer: RemoteSessionIssuer;
}): JSX.Element {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <Dialog.Content>
        {open && (
          <DeleteRemoteIdentityProviderDialogBody
            userSessionIssuerId={userSessionIssuerId}
            issuer={issuer}
            onClose={() => onOpenChange(false)}
          />
        )}
      </Dialog.Content>
    </Dialog>
  );
}

function DeleteRemoteIdentityProviderDialogBody({
  userSessionIssuerId,
  issuer,
  onClose,
}: {
  userSessionIssuerId: string;
  issuer: RemoteSessionIssuer;
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
      // Collect every remote_session_client that joins this
      // user_session_issuer to this remote_session_issuer, walking cursor
      // pagination so a pairing with more than a page's worth of clients is
      // fully drained. The uniqueness is per
      // (project, issuer, user_session_issuer, client_id), so a single
      // pair can have multiple rows.
      const clientIds: string[] = [];
      let cursor: string | undefined;
      do {
        const page = await client.remoteSessionClients.list({
          userSessionIssuerId,
          remoteSessionIssuerId: issuer.id,
          limit: 100,
          cursor,
        });
        for (const remoteClient of page.result.items) {
          clientIds.push(remoteClient.id);
        }
        cursor = page.result.nextCursor || undefined;
      } while (cursor);
      for (const id of clientIds) {
        await client.remoteSessionClients.delete({ id });
      }
      await Promise.all([
        invalidateAllRemoteSessionClients(queryClient, { refetchType: "all" }),
        invalidateAllRemoteSessionIssuers(queryClient, { refetchType: "all" }),
      ]);
      toast.success("Identity provider removed from this server");
      onClose();
    } catch (err) {
      console.error("Delete identity provider failed", err);
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
        <Dialog.Title>Remove Remote Identity Provider</Dialog.Title>
        <Dialog.Description>
          This removes the identity provider's association with this MCP server.
          The provider configuration stays in the project and can be re-attached
          later or used by other servers.
        </Dialog.Description>
      </Dialog.Header>

      <div className="grid gap-2">
        <Type small>
          To confirm, type the issuer URL: <strong>{issuer.issuer}</strong>
        </Type>
        <Input
          value={confirmation}
          onChange={setConfirmation}
          placeholder={issuer.issuer}
          disabled={submitting}
        />
      </div>

      <Alert variant="warning" dismissible={false}>
        Removing {issuer.issuer} from this server cannot be undone — any active
        user sessions issued via this identity provider will need to
        re-authenticate.
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
          {submitting ? (
            <>
              <Button.LeftIcon>
                <Loader2 className="size-4 animate-spin" />
              </Button.LeftIcon>
              <Button.Text>Removing</Button.Text>
            </>
          ) : (
            <Button.Text>Remove</Button.Text>
          )}
        </Button>
      </Dialog.Footer>
    </>
  );
}
