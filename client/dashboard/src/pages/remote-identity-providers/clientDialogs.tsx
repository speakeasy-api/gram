import {
  invalidateAllOrganizationRemoteSessionClients,
  invalidateAllOrganizationRemoteSessionClientSessions,
  invalidateAllOrganizationRemoteSessionIssuers,
  useDeleteOrganizationRemoteSessionClientMutation,
  useOrganizationRemoteSessionClientDeletePreflight,
  useRevokeAllOrganizationRemoteSessionClientSessionsMutation,
} from "@gram/client/react-query/index.js";
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
          request: { revokeAllClientSessionsRequestBody: { clientId } },
        })
      }
    />
  );
}
