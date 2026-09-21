import type { BadgeProps } from "@/components/ui/Badge";
import { chunk } from "@/lib/utils";
import type {
  ClientBinding,
  NotApplicableReason,
  XaaServerReadinessState,
} from "@gram/client/models/components/xaaserverreadiness.js";

type BadgeVariant = NonNullable<BadgeProps["variant"]>;

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

type ConfirmableRow = {
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
