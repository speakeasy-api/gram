import { collectPageItems } from "@/components/sessions/collectPageItems";
import { useSdkClient } from "@/contexts/Sdk";
import { enumPresentation } from "@/lib/enum";
import type { AuthClient } from "@gram/client/models/components/authclient.js";
import { useQuery, type UseQueryResult } from "@tanstack/react-query";

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

export const UNDECIDED_STATUS: StatusPresentation = {
  label: "Not decided",
  hint: "Say whether the upstream needs credentials, so a server with none is never mistaken for one that needs none.",
  tone: "neutral",
};

export const UPSTREAM_AUTH_STATUS: Record<string, StatusPresentation> = {
  configured: {
    label: "Credentials set up",
    hint: "Callers reach the upstream with one of the credentials below.",
    tone: "success",
  },
  setup_incomplete: {
    label: "Authentication setup incomplete",
    hint: "The upstream needs credentials, but none is turned on. Callers can't use this server until one is.",
    tone: "warning",
  },
  no_auth_required: {
    label: "No authentication required",
    hint: "Speakeasy sends no credentials upstream. Callers still need access to the server itself.",
    tone: "neutral",
  },
  undecided: UNDECIDED_STATUS,
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

/**
 * Every auth client in the organization, across all pages. Keyed under the
 * SDK's list key so its invalidation refreshes this too.
 */
export function useAllAuthClients({
  enabled = true,
}: { enabled?: boolean } = {}): UseQueryResult<AuthClient[]> {
  const sdk = useSdkClient();
  return useQuery({
    queryKey: ["@gram/client", "authClients", "list", { all: true }],
    queryFn: ({ signal }) =>
      collectPageItems(
        sdk.authClients.list({ limit: 100 }, undefined, { signal }),
      ),
    enabled,
    throwOnError: false,
  });
}
