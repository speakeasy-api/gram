import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useUserSessionIssuerAttachImpact } from "@gram/client/react-query/userSessionIssuerAttachImpact.js";
import type { AuthTarget } from "./authTarget";

/**
 * Confirms replacing the issuer of a server that already has one. Clients and
 * sessions belong to an issuer, so every connected client signs in again; the
 * server-side impact preview adds whether the authorization server clients are
 * sent to changes too, which also breaks anything configured with it.
 */
export function ChangeIssuerConfirmDialog({
  open,
  onOpenChange,
  target,
  candidate,
  onConfirm,
  isPending,
  error,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  target: AuthTarget;
  candidate: UserSessionIssuer;
  onConfirm: () => void;
  isPending: boolean;
  error: Error | null;
}): JSX.Element {
  const impact = useUserSessionIssuerAttachImpact(
    { ...target.attachImpactTarget, userSessionIssuerId: candidate.id },
    undefined,
    { enabled: open, throwOnError: false, retry: false },
  );

  let summary: string;
  if (impact.isError) {
    summary =
      "Couldn't check whether the authorization server changes too. You can still change the issuer.";
  } else if (impact.data?.authorizationServerChanges) {
    summary = impact.data.candidateIssuer?.issuerUrl
      ? `Clients will also be sent to a different authorization server, ${impact.data.candidateIssuer.issuerUrl}. Update any identity provider or platform set up with the current one.`
      : "Clients will also be sent to a different authorization server. Update any identity provider or platform set up with the current one.";
  } else {
    summary = "Clients keep using the same authorization server.";
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Change user session issuer?"
      description={`Everyone connected to this server will need to sign in again. Their sessions belong to the current issuer and stop working once the server uses ${candidate.slug}.`}
      confirmLabel="Change issuer"
      pendingLabel="Saving…"
      confirmVariant="primary"
      onConfirm={onConfirm}
      isPending={isPending}
      impact={{ summary, isLoading: impact.isPending && !impact.isError }}
      error={error?.message}
    />
  );
}
