import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

import { ApiErrorAlert } from "@/components/api-error-alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import type { OktaResourceConnectionServer } from "@gram/client/models/components/oktaresourceconnectionserver.js";
import { useResetOktaResourceConnectionMutation } from "@gram/client/react-query/resetOktaResourceConnection.js";

import {
  SESSION_SECURITY,
  inlineError,
  invalidateIdentityProviderQueries,
} from "../../identityProviderQueries";

export function ClearConfirmationDialog({
  target,
  canUndo,
  onCleared,
  onClose,
}: {
  target: OktaResourceConnectionServer | null;
  canUndo: boolean;
  onCleared: (row: OktaResourceConnectionServer) => void;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const reset = useResetOktaResourceConnectionMutation({
    onSuccess: () => {
      toast.success(
        "Confirmation cleared. The Okta connection was not changed.",
      );
      if (target) onCleared(target);
      onClose();
      void invalidateIdentityProviderQueries(queryClient);
    },
    onError: inlineError,
  });
  const close = () => {
    if (reset.isPending) return;
    onClose();
    reset.reset();
  };

  return (
    <Dialog
      open={target != null}
      onOpenChange={(next) => {
        if (!next) close();
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Clear this confirmation?</Dialog.Title>
          <Dialog.Description>
            This removes Speakeasy’s saved confirmation and settings for{" "}
            {target?.serverName}. It does not delete or change the connection in
            Okta. Other servers that share this confirmation will also show as
            not confirmed.
            {canUndo
              ? " You can undo this on this page until these settings are confirmed again."
              : " The saved Issuer URL or application is unavailable, so Undo cannot fully restore these settings. Review setup after clearing to choose current settings."}
          </Dialog.Description>
        </Dialog.Header>
        <ApiErrorAlert error={reset.error} />
        <Dialog.Footer>
          <Button
            variant="secondary"
            onClick={close}
            disabled={reset.isPending}
          >
            Cancel
          </Button>
          <Button
            variant="destructive-primary"
            disabled={reset.isPending || target == null}
            onClick={() => {
              if (!target) return;
              reset.mutate({
                security: SESSION_SECURITY,
                request: {
                  resetOktaResourceConnectionRequestBody: {
                    mcpServerId: target.mcpServerId,
                  },
                },
              });
            }}
          >
            {reset.isPending ? "Clearing..." : "Clear confirmation"}
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
