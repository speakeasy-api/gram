import { useDeleteOrganizationRemoteSessionClientMutation } from "@gram/client/react-query/deleteOrganizationRemoteSessionClient.js";
import { useOrganizationRemoteSessionClientDeletePreflight } from "@gram/client/react-query/organizationRemoteSessionClientDeletePreflight.js";
import { invalidateAllOrganizationRemoteSessionClientSessions } from "@gram/client/react-query/organizationRemoteSessionClientSessions.js";
import { invalidateAllOrganizationRemoteSessionClients } from "@gram/client/react-query/organizationRemoteSessionClients.js";
import { invalidateAllOrganizationRemoteSessionIssuers } from "@gram/client/react-query/organizationRemoteSessionIssuers.js";
import { useRevokeAllOrganizationRemoteSessionClientSessionsMutation } from "@gram/client/react-query/revokeAllOrganizationRemoteSessionClientSessions.js";
import { useRotateOrganizationRemoteSessionClientMutation } from "@gram/client/react-query/rotateOrganizationRemoteSessionClient.js";
import { invalidateAllOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ConfirmDialog } from "./ConfirmDialog";

// DeleteClientDialog confirms deletion of a remote session client, surfacing the
// server-side pre-flight (active session count + affected MCP server names).
export function DeleteClientDialog({
  clientId,
  clientLabel,
  onClose,
  onDeleted,
}: {
  clientId: string;
  clientLabel: string;
  onClose: () => void;
  onDeleted?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { data: preflight, isLoading } =
    useOrganizationRemoteSessionClientDeletePreflight({ id: clientId });

  const deleteMutation = useDeleteOrganizationRemoteSessionClientMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllOrganizationRemoteSessionClients(queryClient, {
          refetchType: "all",
        }),
        invalidateAllOrganizationRemoteSessionIssuers(queryClient, {
          refetchType: "all",
        }),
      ]);
      toast.success("Client deleted");
      onDeleted?.();
      onClose();
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to delete client",
      );
    },
  });

  const sessionCount = preflight?.sessionCount ?? 0;

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={`Delete client "${clientLabel}"?`}
      description="This permanently removes the client and revokes every session minted against it."
      confirmLabel="Delete client"
      isPending={deleteMutation.isPending}
      impact={{
        summary: `${sessionCount} ${sessionCount === 1 ? "session" : "sessions"} will be revoked.`,
        mcpServerNames: preflight?.mcpServerNames,
        isLoading,
      }}
      onConfirm={() => deleteMutation.mutate({ request: { id: clientId } })}
    />
  );
}

// RevokeAllSessionsDialog confirms revoking every active session for a client.
export function RevokeAllSessionsDialog({
  clientId,
  onClose,
}: {
  clientId: string;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  // Reuse the client delete pre-flight for an authoritative count of active
  // (non-deleted) sessions; more accurate than the paginated Sessions list.
  const { data: preflight, isLoading } =
    useOrganizationRemoteSessionClientDeletePreflight({ id: clientId });
  const sessionCount = preflight?.sessionCount ?? 0;

  const revokeAll = useRevokeAllOrganizationRemoteSessionClientSessionsMutation(
    {
      onSuccess: async (data) => {
        await invalidateAllOrganizationRemoteSessionClientSessions(
          queryClient,
          { refetchType: "all" },
        );
        toast.success(
          `Revoked ${data.revokedCount} ${data.revokedCount === 1 ? "session" : "sessions"}`,
        );
        onClose();
      },
      onError: (error) => {
        toast.error(
          error instanceof Error ? error.message : "Failed to revoke sessions",
        );
      },
    },
  );

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title="Revoke all sessions?"
      description="Every active session minted against this client will be revoked. Affected principals must re-authenticate."
      confirmLabel="Revoke all"
      isPending={revokeAll.isPending}
      impact={{
        summary: `${sessionCount} ${sessionCount === 1 ? "session" : "sessions"} will be revoked.`,
        isLoading,
      }}
      onConfirm={() =>
        revokeAll.mutate({
          request: { clientId },
        })
      }
    />
  );
}

// RotateClientDialog confirms re-registering a client with its issuer in
// place: the client_id and secret are replaced, the row and its attachments
// stay, and every session minted against the old client_id is revoked.
export function RotateClientDialog({
  clientId,
  clientLabel,
  onClose,
}: {
  clientId: string;
  clientLabel: string;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  // isFetching, not isLoading: a reopened dialog has cached preflight data
  // while it refetches, and a second rotation must not be confirmed against
  // the count from before the first one.
  const { data: preflight, isFetching } =
    useOrganizationRemoteSessionClientDeletePreflight({ id: clientId });
  const sessionCount = preflight?.sessionCount ?? 0;

  const rotate = useRotateOrganizationRemoteSessionClientMutation({
    onSuccess: async () => {
      await Promise.all([
        invalidateAllOrganizationRemoteSessionClient(queryClient, {
          refetchType: "all",
        }),
        invalidateAllOrganizationRemoteSessionClients(queryClient, {
          refetchType: "all",
        }),
        invalidateAllOrganizationRemoteSessionClientSessions(queryClient, {
          refetchType: "all",
        }),
      ]);
      toast.success("Client re-registered with the identity provider");
      onClose();
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to rotate client",
      );
    },
  });

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={`Rotate client "${clientLabel}"?`}
      description="Gram registers a new client with the identity provider and replaces this client's ID and secret in place. Its attachments are kept, but every session minted against the old client is revoked, so users reconnect once."
      confirmLabel="Rotate client"
      isPending={rotate.isPending}
      impact={{
        summary: `${sessionCount} ${sessionCount === 1 ? "session" : "sessions"} will be revoked.`,
        mcpServerNames: preflight?.mcpServerNames,
        isLoading: isFetching,
      }}
      onConfirm={() =>
        rotate.mutate({
          request: { riskIDRequestBody: { id: clientId } },
        })
      }
    />
  );
}
