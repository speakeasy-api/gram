import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useOrganizationUserSessionIssuerAuthenticationHostImpact } from "@gram/client/react-query/organizationUserSessionIssuerAuthenticationHostImpact.js";
import { useSetOrganizationUserSessionIssuerAuthenticationHostMutation } from "@gram/client/react-query/setOrganizationUserSessionIssuerAuthenticationHost.js";
import { useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { invalidateIssuers } from "./invalidateIssuers";

/**
 * Confirms moving an authorization server onto or off the deployment's
 * authentication host. Where clients sign in changes, so the server's preview
 * names the address before and after, and anything set up with the current
 * one must be updated.
 */
export function AuthenticationHostDialog({
  issuer,
  onClose,
}: {
  issuer: UserSessionIssuer;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const moveOn = !issuer.useAuthenticationHost;
  const impact = useOrganizationUserSessionIssuerAuthenticationHostImpact(
    { id: issuer.id, useAuthenticationHost: moveOn },
    undefined,
    { throwOnError: false, retry: false },
  );
  const move = useSetOrganizationUserSessionIssuerAuthenticationHostMutation({
    onSuccess: async (moved) => {
      toast.success(
        moveOn
          ? `${moved.slug} is now on the authentication host`
          : `${moved.slug} is off the authentication host`,
      );
      onClose();
      await invalidateIssuers(queryClient);
    },
  });

  const preview = impact.isFetching || impact.isError ? undefined : impact.data;
  let summary: string;
  if (impact.isError) {
    summary = "Couldn't check what moving changes. Close and try again.";
  } else if (!preview) {
    summary = "";
  } else if (!preview.changes) {
    summary = "Nothing changes: clients already sign in there.";
  } else {
    const servers = `${preview.mcpServerCount} ${preview.mcpServerCount === 1 ? "MCP server uses" : "MCP servers use"} it.`;
    const kind =
      issuer.authorizationServerMode === "shared"
        ? "Its issuer URL and token endpoint change."
        : "MCP servers on a custom domain keep their own address.";
    summary = `Clients will sign in at ${preview.authorizationServerAfter} instead of ${preview.authorizationServerBefore || "each MCP server's own address"}. ${kind} ${servers} Update any identity provider or platform set up with the current address.`;
  }

  let error: string | undefined;
  if (move.isError) {
    error =
      move.error instanceof GramError &&
      (move.error.statusCode === 409 || move.error.statusCode === 400)
        ? "Speakeasy manages this authorization server, or this deployment has no authentication host, so it can't be moved."
        : "Couldn't move it. Try again.";
  }

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={
        moveOn
          ? `Move ${issuer.slug} to the authentication host?`
          : `Move ${issuer.slug} off the authentication host?`
      }
      description={
        moveOn
          ? "Clients sign in on the authentication host instead of the MCP host."
          : "Clients sign in on the MCP host instead of the authentication host."
      }
      confirmLabel="Move"
      pendingLabel="Moving…"
      confirmVariant="primary"
      isPending={move.isPending}
      impact={{ summary, isLoading: impact.isFetching }}
      confirmDisabled={!preview?.changes}
      error={error}
      onConfirm={() =>
        move.mutate({
          request: {
            setAuthenticationHostRequestBody: {
              id: issuer.id,
              useAuthenticationHost: moveOn,
            },
          },
        })
      }
    />
  );
}
