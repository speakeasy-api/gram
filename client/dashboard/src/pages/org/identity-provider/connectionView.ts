import type { BadgeProps } from "@/components/ui/Badge";
import type { IdentityProviderConnectionChecklistItem } from "@gram/client/models/components/identityproviderconnectionchecklistitem.js";
import type {
  LastError,
  OktaIdentityProviderConnection,
  OktaIdentityProviderConnectionListingMode,
  OktaIdentityProviderConnectionStatus,
  VerificationReasons,
} from "@gram/client/models/components/oktaidentityproviderconnection.js";

type BadgeVariant = NonNullable<BadgeProps["variant"]>;

export type LiveConnection = OktaIdentityProviderConnection & {
  status: Exclude<OktaIdentityProviderConnectionStatus, "revoked">;
};

export function isConnected(
  connection: OktaIdentityProviderConnection | null | undefined,
): connection is LiveConnection {
  return connection != null && connection.status !== "revoked";
}

export type ConnectionStep =
  | "submit_client_id"
  | "verify"
  | "repair"
  | "connected";

export function connectionStep(
  connection: Pick<LiveConnection, "status" | "clientIdSubmitted">,
): ConnectionStep {
  if (!connection.clientIdSubmitted) return "submit_client_id";
  if (connection.status === "pending") return "verify";
  if (connection.status === "degraded") return "repair";
  return "connected";
}

/** Mirrors the server: a verification has completed, whether or not it found gaps. */
export function isConnectionChecked(connection: {
  status: OktaIdentityProviderConnectionStatus;
}): boolean {
  return connection.status === "verified" || connection.status === "degraded";
}

/** Mirrors the applications sync and snapshot gate: only a clean verification qualifies. */
export function isConnectionVerified(connection: {
  status: OktaIdentityProviderConnectionStatus;
}): boolean {
  return connection.status === "verified";
}

/** Mirrors the readiness lock: confirmations are accepted on a verified or degraded connection. */
export function canConfirmReadiness(connection: {
  status: OktaIdentityProviderConnectionStatus;
}): boolean {
  return isConnectionChecked(connection);
}

export type ChecklistGroupId = IdentityProviderConnectionChecklistItem["group"];

export type ChecklistGroup = {
  id: ChecklistGroupId;
  title: string;
  description: string;
  items: IdentityProviderConnectionChecklistItem[];
  /** Steps completed according to the server’s verification evidence. */
  completedCount: number;
};

const CHECKLIST_GROUPS: Record<
  ChecklistGroupId,
  { title: string; description: string }
> = {
  connect: {
    title: "Connect",
    description:
      "Set up an API Services app so Speakeasy can connect to Okta, then paste its Client ID to verify access.",
  },
  cross_app_access: {
    title: "Cross App Access setup",
    description:
      "Required for Enterprise Managed Auth: connecting Okta and syncing applications alone does not give AI agents access to your MCP servers. Register the Speakeasy AI agent once, then connect it to each MCP server.",
  },
};

const CHECKLIST_GROUP_ORDER: ChecklistGroupId[] = [
  "connect",
  "cross_app_access",
];

export function groupChecklist(
  items: IdentityProviderConnectionChecklistItem[],
): ChecklistGroup[] {
  return CHECKLIST_GROUP_ORDER.map((id) => {
    const members = items.filter((item) => item.group === id);
    return {
      id,
      ...CHECKLIST_GROUPS[id],
      items: members,
      completedCount: members.filter((item) => item.completed === true).length,
    };
  }).filter((group) => group.items.length > 0);
}

/** Connect until a verification has run, then the agent; nothing once the agent steps are all complete. */
export function activeChecklistGroup(connection: {
  status: OktaIdentityProviderConnectionStatus;
  checklist: IdentityProviderConnectionChecklistItem[];
}): ChecklistGroupId | null {
  if (!isConnectionChecked(connection)) return "connect";
  const agent = groupChecklist(connection.checklist).find(
    (group) => group.id === "cross_app_access",
  );
  const done =
    agent !== undefined && agent.completedCount === agent.items.length;
  return done ? null : "cross_app_access";
}

export const CONNECTION_STATUS: Record<
  OktaIdentityProviderConnectionStatus,
  { label: string; variant: BadgeVariant }
> = {
  pending: { label: "Setup incomplete", variant: "warning" },
  verified: { label: "Verified", variant: "success" },
  degraded: { label: "Needs attention", variant: "destructive" },
  revoked: { label: "Revoked", variant: "neutral" },
};

export const VERIFICATION_REASON_LABELS: Record<VerificationReasons, string> = {
  missing_scope: "Okta did not grant all required permissions (scopes).",
  missing_role:
    "Check the app’s admin role in Okta. Speakeasy could not confirm the required access.",
  dpop_not_bound:
    "Okta did not apply the required token protection (DPoP). Check the app’s DPoP setting in Okta.",
  key_not_fetched:
    "Okta has not retrieved the public signing key. Check that the app uses the public key URL (JWKS) on the Okta Setup tab.",
  "read_failed:okta.apps.read":
    "Speakeasy could not read applications from Okta.",
  "read_failed:okta.users.read": "Speakeasy could not read users from Okta.",
  "read_failed:okta.groups.read": "Speakeasy could not read groups from Okta.",
};

export const LAST_ERROR_LABELS: Record<LastError, string> = {
  credential_rejected:
    "Okta rejected the connection. Check the Client ID and that the app uses the public key URL (JWKS) on the Okta Setup tab.",
  okta_unreachable: "Okta could not be reached during the last verification.",
};

export const LISTING_MODE_LABELS: Record<
  OktaIdentityProviderConnectionListingMode,
  string
> = {
  custom_app: "Custom API Services app",
  oin: "Okta catalog (OIN)",
};

export type DpopObservation = "unknown" | "protected" | "unprotected";

export function dpopObservation(
  connection: Pick<
    OktaIdentityProviderConnection,
    "status" | "lastError" | "checklist" | "dpopRequired"
  >,
): DpopObservation {
  if (!isConnectionChecked(connection) || connection.lastError) {
    return "unknown";
  }
  const observed = connection.checklist?.find((item) => item.key === "dpop");
  if (observed !== undefined && observed.completed === undefined) {
    return "unknown";
  }
  const isProtected = observed?.completed ?? connection.dpopRequired;
  return isProtected ? "protected" : "unprotected";
}
