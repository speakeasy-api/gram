import { Link } from "react-router";
import { Button } from "@/components/ui/Button";
import { useOrgRoutes } from "@/routes";
import {
  SESSION_AUDITOR_ROLE_NAME,
  useSessionAuditAccess,
  type SessionAuditAccess,
} from "./session-audit-access";

/**
 * Hands the permission back. Shared by the Enable logging callout, where the
 * role was taken, and the Confirm traffic note that asks for it back, so the
 * two never drift into different labels for the same write.
 */
export function RemoveSessionAuditAccessButton({
  access,
}: {
  access: SessionAuditAccess;
}): JSX.Element {
  return (
    <Button
      variant="secondary"
      size="sm"
      disabled={access.isPending}
      onClick={access.revoke}
    >
      {access.isPending ? "Removing…" : "Remove my access"}
    </Button>
  );
}

/** Where a directory-synced admin goes to take the role back off themselves. */
export function ScimRemovalGuidance(): JSX.Element {
  const orgRoutes = useOrgRoutes();

  return (
    <p className="text-muted-foreground text-sm leading-relaxed">
      Remove yourself from the directory group mapped to{" "}
      {SESSION_AUDITOR_ROLE_NAME} in your identity provider. Speakeasy
      reconciles the change on the next sync — the mapping itself is in{" "}
      <Link
        to={orgRoutes.identity.href()}
        className="whitespace-nowrap underline underline-offset-2"
      >
        Identity → SCIM → Configure
      </Link>
      .
    </p>
  );
}

/**
 * The prompt to hand {@link SESSION_AUDITOR_ROLE_NAME} back, shown at the end
 * of a Confirm traffic step once traffic has actually arrived. Reading other
 * members' sessions was borrowed to answer one question; this is where the
 * question gets answered, so this is where it is offered back.
 */
export function SessionAuditRevoke(): JSX.Element | null {
  const access = useSessionAuditAccess();

  if (!access.holdsRole) return null;

  return (
    <div className="border-border border p-4">
      <p className="text-foreground text-sm">
        You&apos;re still a {SESSION_AUDITOR_ROLE_NAME}. Now that traffic is
        confirmed, hand the permission back.
      </p>
      <div className="mt-3">
        {access.scimManaged ? (
          <ScimRemovalGuidance />
        ) : (
          <RemoveSessionAuditAccessButton access={access} />
        )}
      </div>
    </div>
  );
}
