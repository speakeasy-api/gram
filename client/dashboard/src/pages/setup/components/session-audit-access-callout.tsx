import { Link } from "react-router";
import { RequireScope } from "@/components/require-scope";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { useOrgRoutes } from "@/routes";
import {
  SESSION_AUDITOR_ROLE_NAME,
  useSessionAuditAccess,
  type SessionAuditAccess,
} from "./session-audit-access";
import { RemoveSessionAuditAccessButton } from "./session-audit-revoke";

const CALLOUT_TITLE = "Sessions are private by default";

type CalloutMode = "hidden" | "scim" | "holding" | "offer";

/**
 * Which face the callout shows. Holding a role that reads sessions wins over
 * already having `chat:read`: the role was taken here, so the way to give it
 * back belongs here too, for as long as it is held. Holding one whose grants
 * have been edited away is not that — it reads nothing, so the offer stands,
 * and taking it repairs the role.
 *
 * Both halves come from the roles query, so they load together; deciding on
 * `canReadSessions` instead would flash the offer at a holder while grants
 * are still in flight.
 */
function calloutMode(access: SessionAuditAccess): CalloutMode {
  if (access.scimManaged) return access.canReadSessions ? "hidden" : "scim";
  if (access.holdsRole && access.roleReadsSessions) return "holding";
  if (access.canReadSessions || !access.available) return "hidden";
  return "offer";
}

/** The permission this is all about, written the way the Access page writes it. */
function ChatReadScope(): JSX.Element {
  return <code className="font-mono">chat:read</code>;
}

function OfferCallout({ access }: { access: SessionAuditAccess }): JSX.Element {
  return (
    <Alert variant="info" alignTop>
      <AlertTitle>{CALLOUT_TITLE}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>
          Not even admins can read another member&apos;s agent sessions.{" "}
          <ChatReadScope /> is a separate permission, meant for the people who
          are supposed to unmask them, and it is never part of Admin.
        </p>
        <p>
          Claude reports the email the account was signed in with, which may not
          be the one you use for Speakeasy — so the conversation you are about
          to send may not read as yours. To confirm traffic, Speakeasy will
          create a {SESSION_AUDITOR_ROLE_NAME} role carrying <ChatReadScope />{" "}
          and add you to it using your own admin access. Every change is
          recorded in the audit log. Remove yourself once traffic shows up.
        </p>
        <RequireScope scope="org:admin" level="component">
          <Button size="sm" disabled={access.isPending} onClick={access.grant}>
            {access.isPending
              ? "Adding…"
              : `Add me as ${SESSION_AUDITOR_ROLE_NAME}`}
          </Button>
        </RequireScope>
      </AlertDescription>
    </Alert>
  );
}

// Under directory sync the identity provider is the source of truth for role
// assignment: a membership written here is replaced on the next sync. So the
// role is still created — the identity provider needs something to map to —
// and the membership is asked for where it will actually hold.
function ScimCallout({ access }: { access: SessionAuditAccess }): JSX.Element {
  const orgRoutes = useOrgRoutes();

  return (
    <Alert variant="info" alignTop>
      <AlertTitle>{CALLOUT_TITLE}</AlertTitle>
      <AlertDescription className="space-y-3">
        <p>
          Not even admins can read another member&apos;s agent sessions.{" "}
          <ChatReadScope /> is a separate permission, meant for the people who
          are supposed to unmask them, and it is never part of Admin. Claude
          reports the email the account was signed in with, which may not be the
          one you use for Speakeasy, so the conversation you are about to send
          may not read as yours.
        </p>
        <p>
          Your identity provider assigns roles for this organization, so
          Speakeasy can create the {SESSION_AUDITOR_ROLE_NAME} role but cannot
          add you to it. Map a directory group — &ldquo;Session Auditors&rdquo;,
          say — to {SESSION_AUDITOR_ROLE_NAME} from{" "}
          <Link
            to={orgRoutes.identity.href()}
            className="whitespace-nowrap underline underline-offset-2"
          >
            Identity → SCIM → Configure
          </Link>
          , add yourself to the group, and remove yourself from it once traffic
          shows up.
        </p>
        <RequireScope scope="org:admin" level="component">
          <Button
            size="sm"
            disabled={access.isPending || access.roleExists}
            onClick={access.ensureRole}
          >
            {access.roleExists
              ? "Created"
              : `Create ${SESSION_AUDITOR_ROLE_NAME} role`}
          </Button>
        </RequireScope>
      </AlertDescription>
    </Alert>
  );
}

function HoldingStatus({
  access,
}: {
  access: SessionAuditAccess;
}): JSX.Element {
  return (
    <Alert variant="info">
      <div className="flex flex-wrap items-center gap-3">
        <span className="text-foreground text-sm">
          You hold {SESSION_AUDITOR_ROLE_NAME} access. Remove it after
          confirming traffic.
        </span>
        <RequireScope scope="org:admin" level="component">
          <RemoveSessionAuditAccessButton access={access} />
        </RequireScope>
      </div>
    </Alert>
  );
}

/**
 * Explains, at the step that turns logging on, why the traffic an admin is
 * about to send may stay invisible to them — and offers the temporary role
 * that makes it visible. Sits here rather than at Confirm traffic because
 * grants are read per request: taken now, the next poll already runs under it.
 */
export function SessionAuditAccessCallout(): JSX.Element | null {
  const access = useSessionAuditAccess();

  switch (calloutMode(access)) {
    case "hidden":
      return null;
    case "scim":
      return <ScimCallout access={access} />;
    case "holding":
      return <HoldingStatus access={access} />;
    case "offer":
      return <OfferCallout access={access} />;
  }
}
