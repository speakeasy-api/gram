import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useMoveOrganizationUserSessionIssuerMutation } from "@gram/client/react-query/moveOrganizationUserSessionIssuer.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { invalidateIssuers } from "./invalidateIssuers";

/**
 * Confirms sharing a project's authorization server with every project. It
 * keeps its MCP servers, clients, sessions, and issuer URL; what changes is
 * who can choose it, and the page offers no way back to one project.
 */
export function MakeOrganizationWideDialog({
  issuer,
  onClose,
}: {
  issuer: UserSessionIssuer;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const move = useMoveOrganizationUserSessionIssuerMutation({
    onSuccess: async (moved) => {
      toast.success(`${moved.slug} is now organization-wide`);
      onClose();
      await invalidateIssuers(queryClient);
    },
  });

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={`Make ${issuer.slug} organization-wide?`}
      description="Every project in the organization will be able to choose it for its MCP servers. Its servers, clients, and sessions stay as they are."
      confirmLabel="Make organization-wide"
      pendingLabel="Saving…"
      confirmVariant="primary"
      isPending={move.isPending}
      error={
        move.isError
          ? move.error instanceof GramError && move.error.statusCode === 409
            ? "Speakeasy manages this authorization server, or it changed while saving, so it can't be made organization-wide. Reload the page to check it."
            : "Couldn't make it organization-wide. Try again."
          : undefined
      }
      onConfirm={() =>
        move.mutate({ request: { moveIssuerRequestBody: { id: issuer.id } } })
      }
    />
  );
}
