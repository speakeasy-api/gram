import type { BadgeProps } from "@/components/ui/Badge";
import type { IdentityProviderConnectionChecklistItem } from "@gram/client/models/components/identityproviderconnectionchecklistitem.js";
import { capitalize } from "@/lib/utils";
import type {
  ErrorT as ReconcileRunError,
  IdentityProviderConnectionReconcileRunStatus,
} from "@gram/client/models/components/identityproviderconnectionreconcilerun.js";
import type {
  LastError,
  OktaIdentityProviderConnectionListingMode,
  OktaIdentityProviderConnectionStatus,
  VerificationReasons,
} from "@gram/client/models/components/oktaidentityproviderconnection.js";
import type {
  ClientBinding,
  NotApplicableReason,
  XaaServerReadinessState,
} from "@gram/client/models/components/xaaserverreadiness.js";

type BadgeVariant = NonNullable<BadgeProps["variant"]>;

export type ConnectionStep =
  | "submit_client_id"
  | "verify"
  | "connected"
  | "revoked";

export function connectionStep(connection: {
  status: OktaIdentityProviderConnectionStatus;
  clientIdSubmitted: boolean;
}): ConnectionStep {
  if (connection.status === "revoked") return "revoked";
  if (!connection.clientIdSubmitted) return "submit_client_id";
  if (connection.status === "pending") return "verify";
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

/** The Okta admin console for an org URL (`acme.okta.com` → `acme-admin.okta.com`). */
export function oktaAdminConsoleUrl(orgUrl: string): string {
  const normalized = normalizeOktaOrgUrl(orgUrl);
  if (!normalized) return orgUrl;
  for (const suffix of OKTA_ORG_HOST_SUFFIXES) {
    if (normalized.endsWith(`.${suffix}`)) {
      const tenant = normalized
        .slice(0, -(suffix.length + 1))
        .replace(/-admin$/, "");
      return `${tenant}-admin.${suffix}`;
    }
  }
  return orgUrl;
}

export function pluralize(count: number, noun: string): string {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

const STATUS_VARIANTS: Record<
  OktaIdentityProviderConnectionStatus,
  BadgeVariant
> = {
  pending: "warning",
  verified: "success",
  degraded: "destructive",
  revoked: "neutral",
};

const STATUS_LABELS: Record<OktaIdentityProviderConnectionStatus, string> = {
  pending: "Setup incomplete",
  verified: "Verified",
  degraded: "Needs attention",
  revoked: "Revoked",
};

export function connectionStatusVariant(
  status: OktaIdentityProviderConnectionStatus,
): BadgeVariant {
  return STATUS_VARIANTS[status];
}

export function connectionStatusLabel(
  status: OktaIdentityProviderConnectionStatus,
): string {
  return STATUS_LABELS[status];
}

const VERIFICATION_REASONS: Record<VerificationReasons, string> = {
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

export function verificationReasonLabel(reason: VerificationReasons): string {
  return VERIFICATION_REASONS[reason];
}

const LAST_ERRORS: Record<LastError, string> = {
  credential_rejected:
    "Okta rejected the connection. Check the Client ID and that the app uses the public key URL (JWKS) on the Okta Setup tab.",
  okta_unreachable: "Okta could not be reached during the last verification.",
};

export function lastErrorLabel(error: LastError): string {
  return LAST_ERRORS[error];
}

const LISTING_MODES: Record<OktaIdentityProviderConnectionListingMode, string> =
  {
    custom_app: "Custom API Services app",
    oin: "Okta catalog (OIN)",
  };

export function listingModeLabel(
  mode: OktaIdentityProviderConnectionListingMode,
): string {
  return LISTING_MODES[mode];
}

const OKTA_ORG_HOST_SUFFIXES = [
  "okta.com",
  "oktapreview.com",
  "okta-emea.com",
  "okta.mil",
] as const;

const HOST_LABEL = "[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?";
const PLAIN_HOSTNAME = new RegExp(`^${HOST_LABEL}(?:\\.${HOST_LABEL})+$`);

/** Mirrors the create API and okta.parseOrgURL: https, an ASCII plain hostname (no userinfo, port, path beyond one trailing slash, query, or fragment) that is a subdomain of an Okta-owned suffix. */
export function normalizeOktaOrgUrl(input: string): string | undefined {
  const match = /^https:\/\/([^/?#\s@:]+)\/?$/i.exec(input.trim());
  const rawHost = match?.[1];
  if (!rawHost || !PLAIN_HOSTNAME.test(rawHost)) return undefined;
  const host = rawHost.toLowerCase();
  const ok = OKTA_ORG_HOST_SUFFIXES.some((suffix) =>
    host.endsWith(`.${suffix}`),
  );
  return ok ? `https://${host}` : undefined;
}

const RECONCILE_ERRORS: Record<ReconcileRunError, string> = {
  rate_limited: "Okta received too many requests. Try syncing again later.",
  credential_rejected:
    "Okta rejected the connection. Review the setup on the Okta Setup tab.",
  okta_unreachable: "Okta could not be reached.",
  client_unavailable:
    "Speakeasy could not connect to Okta to sync applications.",
  superseded: "A newer sync finished first. Its results were kept.",
  discarded: "Sync stopped because the Okta connection was no longer verified.",
  interrupted: "Sync was interrupted before it finished. Try syncing again.",
};

export function reconcileErrorLabel(error: ReconcileRunError): string {
  return RECONCILE_ERRORS[error];
}

const RECONCILE_STATUS_VARIANTS: Record<
  IdentityProviderConnectionReconcileRunStatus,
  BadgeVariant
> = {
  running: "information",
  succeeded: "success",
  failed: "destructive",
};

const RECONCILE_STATUS_LABELS: Record<
  IdentityProviderConnectionReconcileRunStatus,
  string
> = {
  running: "Running",
  succeeded: "Succeeded",
  failed: "Failed",
};

export function reconcileStatusVariant(
  status: IdentityProviderConnectionReconcileRunStatus,
): BadgeVariant {
  return RECONCILE_STATUS_VARIANTS[status];
}

export function reconcileStatusLabel(
  status: IdentityProviderConnectionReconcileRunStatus,
): string {
  return RECONCILE_STATUS_LABELS[status];
}

const SIGN_ON_MODES: Record<string, string> = {
  OPENID_CONNECT: "OpenID Connect",
  SAML_2_0: "SAML 2.0",
  SAML_1_1: "SAML 1.1",
  WS_FEDERATION: "WS-Federation",
  BROWSER_PLUGIN: "Browser plugin",
  SECURE_PASSWORD_STORE: "Secure password store",
  AUTO_LOGIN: "Auto login",
  BASIC_AUTH: "Basic auth",
  BOOKMARK: "Bookmark",
};

/** Okta's SCREAMING_SNAKE tokens (sign-on modes, statuses) as readable labels. */
export function humanizeOktaToken(token: string): string {
  const known = SIGN_ON_MODES[token];
  if (known) return known;
  return capitalize(token.toLowerCase().replace(/_/g, " "));
}

export function applicationStatusVariant(status: string): BadgeVariant {
  return status === "ACTIVE" ? "success" : "neutral";
}

const XAA_STATE_LABELS: Record<XaaServerReadinessState, string> = {
  not_applicable: "Not applicable",
  needs_agent: "Needs agent",
  needs_connection: "Not confirmed",
  connected: "Confirmed",
};

const XAA_STATE_VARIANTS: Record<XaaServerReadinessState, BadgeVariant> = {
  not_applicable: "neutral",
  needs_agent: "warning",
  needs_connection: "warning",
  connected: "success",
};

export function xaaStateLabel(state: XaaServerReadinessState): string {
  return XAA_STATE_LABELS[state];
}

export function xaaStateVariant(state: XaaServerReadinessState): BadgeVariant {
  return XAA_STATE_VARIANTS[state];
}

const NOT_APPLICABLE_REASONS: Record<NotApplicableReason, string> = {
  no_idjag:
    "This server does not report support for the identity assertion grant, the sign-in method required for Cross App Access.",
};

/** Short form for the table cell; the full sentence goes in the tooltip. */
const NOT_APPLICABLE_SUMMARIES: Record<NotApplicableReason, string> = {
  no_idjag: "Cross App Access support not reported.",
};

export function notApplicableReasonSummary(
  reason: NotApplicableReason | undefined,
): string {
  if (reason === undefined) return "No Okta connection needed.";
  return NOT_APPLICABLE_SUMMARIES[reason];
}

export function notApplicableReasonLabel(
  reason: NotApplicableReason | undefined,
): string {
  if (reason === undefined) {
    return "No Okta connection is needed for this server.";
  }
  return NOT_APPLICABLE_REASONS[reason];
}

const CLIENT_BINDING_NOTES: Record<ClientBinding, string | undefined> = {
  bound: undefined,
  single: undefined,
  missing:
    "No app registration (OAuth client) was found for this MCP server. Ask the server administrator to register one before confirming.",
  ambiguous:
    "Several app registrations (OAuth clients) were found for this MCP server. Ask the server administrator to select one before confirming.",
};

export function clientBindingNote(binding: ClientBinding): string | undefined {
  return CLIENT_BINDING_NOTES[binding];
}

export type AppInstanceOption = { id: string; label: string };

/** Labels for the app-instance picker; duplicate labels get the Okta app id so they can be told apart. */
export function appInstanceOptions(
  applications: { oktaAppId: string; label: string }[],
): AppInstanceOption[] {
  const counts = new Map<string, number>();
  for (const app of applications) {
    counts.set(app.label, (counts.get(app.label) ?? 0) + 1);
  }
  return applications.map((app) => ({
    id: app.oktaAppId,
    label:
      (counts.get(app.label) ?? 0) > 1
        ? `${app.label} · ${app.oktaAppId}`
        : app.label,
  }));
}

export type ConfirmableRow = {
  pending: boolean;
  state: XaaServerReadinessState;
  clientBinding: ClientBinding;
};

/** A row the admin can confirm: still pending, past the agent step, and with one client to name. */
export function isConfirmable(row: ConfirmableRow): boolean {
  return (
    row.pending &&
    row.state === "needs_connection" &&
    row.clientBinding !== "missing" &&
    row.clientBinding !== "ambiguous"
  );
}

/** Mirrors the confirm API: the resource app's issuer URL, https, no query or fragment. */
export const AUDIENCE_MAX_RUNES = 512;

export function normalizeAudience(input: string): string | undefined {
  const trimmed = input.trim();
  if (Array.from(trimmed).length > AUDIENCE_MAX_RUNES) return undefined;
  if (!/^https:\/\/[^\s?#]+$/i.test(trimmed)) return undefined;
  try {
    const url = new URL(trimmed);
    if (url.username || url.password) return undefined;
    return trimmed;
  } catch {
    return undefined;
  }
}

export type ConfirmRequestBody = {
  connections: {
    mcpServerId: string;
    audience: string;
    oktaApplicationId?: string;
  }[];
};

/** One audience per bar submission; the selection is split into batches of the API cap. */
export function buildConfirmRequests(
  rows: { mcpServerId: string }[],
  audience: string,
  oktaApplicationId: string | undefined,
): ConfirmRequestBody[] {
  return chunk(rows, XAA_CONFIRM_BATCH).map((batch) => ({
    connections: batch.map((row) => ({
      mcpServerId: row.mcpServerId,
      audience,
      ...(oktaApplicationId ? { oktaApplicationId } : {}),
    })),
  }));
}

export function chunk<T>(items: T[], size: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < items.length; i += size) {
    out.push(items.slice(i, i + size));
  }
  return out;
}

export const XAA_CONFIRM_BATCH = 200;
