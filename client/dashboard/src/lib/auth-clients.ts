import { enumPresentation } from "@/lib/enum";
import type { AuthClient } from "@gram/client/models/components/authclient.js";

/**
 * How Speakeasy reaches an upstream on a caller's behalf, in the words an
 * administrator uses. The backend calls these auth clients; the dashboard
 * calls them upstream credentials.
 */
const CLIENT_TYPE_LABEL: Record<string, string> = {
  remote_session: "Each person's own account",
  service_account: "Service account",
  static_credential: "Static credential",
  pass_through: "Caller-supplied header",
  ema: "Enterprise-managed sign-in",
};

const CLIENT_TYPE_HINT: Record<string, string> = {
  remote_session:
    "Everyone connects their own upstream account. Nobody can use another person's connection.",
  service_account:
    "Everyone who may use it acts as the same upstream identity. The upstream records their actions as that identity.",
  static_credential:
    "Everyone who may use it sends the same stored secret. The upstream records their actions as that identity.",
  pass_through:
    "The calling app sends its own header, and Speakeasy forwards it upstream.",
  ema: "Your identity provider vouches for the person, so nobody signs in to the upstream.",
};

export function clientTypeLabel(clientType: string): string {
  return enumPresentation(CLIENT_TYPE_LABEL, clientType, "Upstream credential");
}

export function clientTypeHint(clientType: string): string {
  return enumPresentation(CLIENT_TYPE_HINT, clientType, "");
}

/** The client types a role can be allowed or blocked from, in display order. */
export const CLIENT_TYPES = [
  "remote_session",
  "service_account",
  "static_credential",
  "pass_through",
  "ema",
] as const;

type StatusPresentation = {
  label: string;
  hint: string;
  tone: "success" | "warning" | "neutral";
};

export const NO_AUTH_STATUS: StatusPresentation = {
  label: "No authentication required",
  hint: "Speakeasy sends no credentials upstream. Callers still need access to the server itself.",
  tone: "neutral",
};

/**
 * The upstream asks for sign-in, but the server has no credential turned on to
 * send it.
 */
export const SIGN_IN_DETECTED_STATUS: StatusPresentation = {
  label: "Authentication setup incomplete",
  hint: "The upstream needs credentials, but none is set up and turned on. Add or turn on a credential below.",
  tone: "warning",
};

export const UPSTREAM_AUTH_STATUS: Record<string, StatusPresentation> = {
  configured: {
    label: "Credentials set up",
    hint: "Callers reach the upstream with one of the credentials below.",
    tone: "success",
  },
  setup_incomplete: SIGN_IN_DETECTED_STATUS,
  no_auth_required: NO_AUTH_STATUS,
};

/** The other servers a change to this client also changes. */
export function otherServers(
  client: AuthClient,
  mcpServerId: string,
): AuthClient["servers"] {
  return client.servers.filter((server) => server.id !== mcpServerId);
}

export function serverLabel(server: AuthClient["servers"][number]): string {
  return server.name?.trim() || server.slug?.trim() || "Unnamed server";
}
