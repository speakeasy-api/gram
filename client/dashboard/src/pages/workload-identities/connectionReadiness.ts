import type { NotReadyReason } from "@gram/client/models/components/workloadconnectionendpoint.js";

export const NOT_READY_MESSAGES: Record<NotReadyReason, string> = {
  not_publicly_reachable:
    "This MCP server can't be reached publicly: it is disabled, or reachable only through a private network. A platform's token exchange will fail.",
  no_authorization_server:
    "Gram isn't this MCP server's authorization server, because the server isn't protected by Gram sign-in. There is no token endpoint to exchange at until it is.",
  workload_grant_unavailable:
    "This MCP server's authorization server doesn't advertise the jwt-bearer grant, so it accepts no workload identity tokens. Every exchange will fail, whatever the platform is configured with.",
  agent_rollout_disabled:
    "Agent authorization isn't enabled for this organization, and the token endpoint refuses workload identity tokens without it. The values below are correct, but every exchange will fail until it is enabled.",
};

export const NOT_READY_SHORT: Record<NotReadyReason, string> = {
  not_publicly_reachable: "Not publicly reachable",
  no_authorization_server: "Not protected by Gram sign-in",
  workload_grant_unavailable: "Accepts no workload tokens",
  agent_rollout_disabled: "Agent authorization is off",
};

// The reasons after which there is no token endpoint worth copying.
export const NO_VALUES: ReadonlySet<NotReadyReason> = new Set<NotReadyReason>([
  "not_publicly_reachable",
  "no_authorization_server",
]);
