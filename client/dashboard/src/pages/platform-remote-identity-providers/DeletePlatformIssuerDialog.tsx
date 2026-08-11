import { useDeleteGlobalRemoteSessionIssuerMutation } from "@gram/client/react-query/deleteGlobalRemoteSessionIssuer.js";
import { invalidateAllGlobalRemoteSessionIssuers } from "@gram/client/react-query/globalRemoteSessionIssuers.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { ConfirmDialog } from "../remote-identity-providers/ConfirmDialog";
import { blockerSummary } from "./blockerSummary";

export function DeletePlatformIssuerDialog({
  issuerId,
  issuerLabel,
  globalClientCount,
  tenantClientCount,
  onClose,
  onDeleted,
}: {
  issuerId: string;
  issuerLabel: string;
  globalClientCount: number;
  tenantClientCount: number;
  onClose: () => void;
  onDeleted?: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();

  const deleteMutation = useDeleteGlobalRemoteSessionIssuerMutation({
    onSuccess: async () => {
      await invalidateAllGlobalRemoteSessionIssuers(queryClient, {
        refetchType: "all",
      });
      toast.success("Platform identity provider deleted");
      onDeleted?.();
      onClose();
    },
  });

  // The server refuses the delete when a client was registered between the read
  // that produced these counts and the attempt, and its message names the two
  // populations. Keep it inline and leave the dialog open: it is a refusal to
  // read and act on, not a transient failure to dismiss.
  const deleteError = deleteMutation.error
    ? deleteMutation.error instanceof Error && deleteMutation.error.message
      ? deleteMutation.error.message
      : "An unexpected error occurred. Please try again."
    : null;

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={`Delete "${issuerLabel}"?`}
      description="This removes the provider from the platform catalog for every organization. It is refused while any client is still registered with it."
      confirmLabel="Delete provider"
      isPending={deleteMutation.isPending}
      impact={{
        summary: blockerSummary(globalClientCount, tenantClientCount),
      }}
      error={deleteError}
      onConfirm={() => deleteMutation.mutate({ request: { id: issuerId } })}
    />
  );
}
