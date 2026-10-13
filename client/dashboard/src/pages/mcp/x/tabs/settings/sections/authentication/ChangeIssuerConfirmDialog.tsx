import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import type { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";
import { useUserSessionIssuerAttachImpact } from "@gram/client/react-query/userSessionIssuerAttachImpact.js";
import type { AuthTarget } from "./authTarget";

/**
 * Confirms attaching an issuer to an existing server. Replacing one makes every
 * connected client sign in again, because clients and sessions belong to an
 * issuer; attaching the first one starts requiring clients to sign in at all.
 * The server-side impact preview adds where clients are sent to sign in.
 */
export function ChangeIssuerConfirmDialog({
  open,
  onOpenChange,
  target,
  replacing,
  candidate,
  onConfirm,
  isPending,
  error,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  target: AuthTarget;
  /** Whether the server has an issuer now, which the candidate replaces. */
  replacing: boolean;
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

  const candidateIssuerUrl = impact.data?.candidateIssuer?.issuerUrl;
  let summary: string;
  if (impact.isError) {
    summary = replacing
      ? "Couldn't check whether the authorization server changes too. You can still change the issuer."
      : "Couldn't check where clients will sign in. You can still require sign-in.";
  } else if (!replacing) {
    summary = candidateIssuerUrl
      ? `Clients will sign in through the shared authorization server, ${candidateIssuerUrl}.`
      : "Clients will sign in through this server's own authorization server.";
  } else if (impact.data?.authorizationServerChanges) {
    summary = candidateIssuerUrl
      ? `Clients will also be sent to a different authorization server, ${candidateIssuerUrl}. Update any identity provider or platform set up with the current one.`
      : "Clients will also be sent to a different authorization server. Update any identity provider or platform set up with the current one.";
  } else {
    summary = "Clients keep using the same authorization server.";
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={
        replacing
          ? "Change user session issuer?"
          : "Require sign-in for this server?"
      }
      description={
        replacing
          ? `Everyone connected to this server will need to sign in again. Their sessions belong to the current issuer and stop working once the server uses ${candidate.slug}.`
          : `Clients connected to this server will need to sign in through ${candidate.slug} before they can use it again.`
      }
      confirmLabel={replacing ? "Change issuer" : "Require sign-in"}
      pendingLabel="Saving…"
      confirmVariant="primary"
      onConfirm={onConfirm}
      isPending={isPending}
      // isFetching, not isPending: reopening refetches over a cached preview,
      // and confirming must wait for the current one.
      impact={{ summary, isLoading: impact.isFetching }}
      error={error?.message}
    />
  );
}
