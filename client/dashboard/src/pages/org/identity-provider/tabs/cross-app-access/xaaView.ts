import type { BadgeProps } from "@/components/ui/Badge";
import { chunk } from "@/lib/utils";
import type {
  BrokenReason,
  ClientBinding,
  NotApplicableReason,
  ObservedResult,
  OktaResourceConnectionServerState,
} from "@gram/client/models/components/oktaresourceconnectionserver.js";

type BadgeVariant = NonNullable<BadgeProps["variant"]>;

const XAA_STATE_LABELS: Record<OktaResourceConnectionServerState, string> = {
  not_applicable: "Not applicable",
  needs_agent: "Needs agent",
  needs_connection: "Not confirmed",
  broken: "Not working",
  connected: "Confirmed",
  verified: "Verified",
};

const XAA_STATE_VARIANTS: Record<
  OktaResourceConnectionServerState,
  BadgeVariant
> = {
  not_applicable: "neutral",
  needs_agent: "warning",
  needs_connection: "warning",
  broken: "destructive",
  connected: "success",
  verified: "success",
};

export function xaaStateLabel(
  state: OktaResourceConnectionServerState,
): string {
  return XAA_STATE_LABELS[state];
}

export function xaaStateVariant(
  state: OktaResourceConnectionServerState,
): BadgeVariant {
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

/** Short form for the table cell; the full sentence goes in the tooltip. */
const BROKEN_REASON_SUMMARIES: Record<BrokenReason, string> = {
  audience_mismatch: "Issuer URL does not match.",
  scope_not_allowed: "Scopes not allowed.",
  client_auth_failed: "Agent sign-in rejected.",
  downstream_rejected: "Server refused Okta's assertion.",
};

const BROKEN_REASONS: Record<BrokenReason, string> = {
  audience_mismatch:
    "The confirmed issuer URL is not this server's authorization server issuer, which Speakeasy requests the assertion for. If it was mistyped, confirm again with the issuer; otherwise Speakeasy cannot complete this exchange yet.",
  scope_not_allowed:
    "Okta refused the requested scopes. Allow them on the AI agent's resource connection.",
  client_auth_failed:
    "Okta rejected the AI agent app's client authentication. Check the agent app's credentials in Okta.",
  downstream_rejected:
    "Okta issued the assertion, but this server's authorization server refused it. The server's own workspace must trust your Okta organization.",
};

export function brokenReasonSummary(reason: BrokenReason | undefined): string {
  if (reason === undefined) return "";
  return BROKEN_REASON_SUMMARIES[reason];
}

export function brokenReasonLabel(reason: BrokenReason | undefined): string {
  if (reason === undefined) return "";
  return BROKEN_REASONS[reason];
}

/** States in which a recorded confirmation is shown and can be reviewed or cleared. */
const CONFIRMED_STATES: ReadonlySet<OktaResourceConnectionServerState> =
  new Set(["connected", "verified", "broken", "needs_connection"]);

/** A confirmation is recorded for the row's upstream, whatever the exchange later showed. */
export function hasConfirmation(row: {
  state: OktaResourceConnectionServerState;
  confirmedAt?: Date | undefined;
}): boolean {
  return row.confirmedAt !== undefined && CONFIRMED_STATES.has(row.state);
}

export type StateNoteCopy = { summary: string; tooltip: string };

/** Why a confirmed row reads as not connected again, if an exchange said so. */
export function observedNote(row: {
  state: OktaResourceConnectionServerState;
  observedResult?: ObservedResult | undefined;
}): StateNoteCopy | undefined {
  if (
    row.state === "needs_connection" &&
    row.observedResult === "connection_missing"
  ) {
    return {
      summary: "Okta rejected the exchange.",
      tooltip:
        "Okta rejected the last exchange. Check that the connection exists in Okta, then confirm again.",
    };
  }
  return undefined;
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

type ConfirmableRow = {
  pending: boolean;
  state: OktaResourceConnectionServerState;
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

export const XAA_CONFIRM_BATCH = 200;

type ConfirmRequestBody = {
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
  batchSize = XAA_CONFIRM_BATCH,
): ConfirmRequestBody[] {
  return chunk(rows, batchSize).map((batch) => ({
    connections: batch.map((row) => ({
      mcpServerId: row.mcpServerId,
      audience,
      ...(oktaApplicationId ? { oktaApplicationId } : {}),
    })),
  }));
}
