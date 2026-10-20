import { getServerURL } from "@/lib/utils";
import type { AgentPolicyGrant } from "@gram/client/models/components/agentpolicygrant.js";
import type { AgentPolicyGrantForm } from "@gram/client/models/components/agentpolicygrantform.js";
import type { WorkloadAdmission } from "@gram/client/models/components/workloadadmission.js";
import type { WorkloadDeviceJoinTokenSummary } from "@gram/client/models/components/workloaddevicejointokensummary.js";

/**
 * Enrolling a headless device: a box with no platform identity of its own (a
 * VPS, a sandbox) that joins as an agent's workload by redeeming a single-use
 * bootstrap token. The server calls these "device join tokens"; every word a
 * reader sees says "bootstrap token".
 */

/** The workload picker's key for this flow. Not a catalog entry. */
export const HEADLESS_DEVICE_KEY = "headless-device";

/** The scope an enrolled device polls its policy with. Enrolling needs it. */
export const DEVICE_SYNC_SCOPE = "org:device_agent_sync";

/** The scope that decides whether a device's hook events are accepted. */
export const HOOKS_INGEST_SCOPE = "org:hooks_ingest";

/**
 * The issuer Speakeasy creates for an organization's enrolled devices, and the
 * prefix it gives their subjects. Mirrors deviceIssuerName and
 * deviceSubjectPrefix in server/internal/workloadpolicy/devices.go.
 */
export const DEVICE_ISSUER_NAME = "Speakeasy devices";
export const DEVICE_SUBJECT_PREFIX = "device:";

/** The production control plane, which the device binary assumes by default. */
export const DEFAULT_CONTROL_PLANE_URL = "https://app.getgram.ai";

/** How long a bootstrap token stays redeemable. The server allows 60s–24h. */
export const TOKEN_LIFETIMES = [
  { seconds: 15 * 60, label: "15 minutes" },
  { seconds: 60 * 60, label: "1 hour" },
  { seconds: 24 * 60 * 60, label: "24 hours" },
] as const;
export const DEFAULT_TOKEN_LIFETIME_SECONDS = 60 * 60;

export function tokenLifetimeLabel(seconds: number): string {
  return (
    TOKEN_LIFETIMES.find((option) => option.seconds === seconds)?.label ??
    `${Math.round(seconds / 60)} minutes`
  );
}

/** Where the device configuration lives on the box. */
export const MANAGED_CONFIG_PATH = "/etc/speakeasy/managed.json";

/**
 * The policy grants a headless device needs from the agent it enrolls under.
 * Device sync always: without it the token is refused. Hooks for one project
 * only when asked for: enrolling does not need it, and a device that only uses
 * MCP should not be able to write hook events anywhere.
 */
export function deviceEnrollmentPolicyGrants({
  organizationId,
  hooksProjectId,
}: {
  organizationId: string;
  hooksProjectId?: string;
}): AgentPolicyGrantForm[] {
  const grants: AgentPolicyGrantForm[] = [
    {
      effect: "allow",
      scope: DEVICE_SYNC_SCOPE,
      selector: { resourceKind: "org", resourceId: organizationId },
    },
  ];
  if (hooksProjectId)
    grants.push(hooksIngestGrant(organizationId, hooksProjectId));
  return grants;
}

/** Accept hook events from this agent's devices for one project. */
export function hooksIngestGrant(
  organizationId: string,
  projectId: string,
): AgentPolicyGrantForm {
  return {
    effect: "allow",
    scope: HOOKS_INGEST_SCOPE,
    selector: { resourceKind: "org", resourceId: organizationId, projectId },
  };
}

const ANY = "*";

/** Whether an allow grant for `scope` covers the organization. */
function coversOrganization(
  grant: AgentPolicyGrant,
  scope: string,
  organizationId: string,
): boolean {
  return (
    grant.effect === "allow" &&
    grant.scope === scope &&
    (grant.selector.resourceId === organizationId ||
      grant.selector.resourceId === ANY)
  );
}

/** Whether the agent's policy lets its devices enroll and sync. */
export function allowsDeviceSync(
  grants: AgentPolicyGrant[],
  organizationId: string,
): boolean {
  return grants.some((grant) =>
    coversOrganization(grant, DEVICE_SYNC_SCOPE, organizationId),
  );
}

/** Whether the agent's policy accepts hook events for this project. */
export function acceptsHooks(
  grants: AgentPolicyGrant[],
  organizationId: string,
  projectId: string,
): boolean {
  return grants.some(
    (grant) =>
      coversOrganization(grant, HOOKS_INGEST_SCOPE, organizationId) &&
      (grant.selector.projectId === undefined ||
        grant.selector.projectId === ANY ||
        grant.selector.projectId === projectId),
  );
}

/**
 * The file a device reads on start. The token sits at the top level, beside
 * the workload identity rather than inside it.
 */
export function managedConfig({
  joinToken,
  hooksProjectSlug,
  controlPlaneURL,
}: {
  joinToken: string;
  hooksProjectSlug: string;
  /** The API origin this dashboard talks to. */
  controlPlaneURL: string;
}): string {
  const config: Record<string, unknown> = {
    v: 1,
    workload_identity: {
      source: "device_key",
      hooks_project_slug: hooksProjectSlug,
    },
    join_token: joinToken,
  };
  const origin = originOf(controlPlaneURL);
  if (origin !== null && origin !== DEFAULT_CONTROL_PLANE_URL) {
    config["_control_plane_url"] = origin;
  }
  return JSON.stringify(config, null, 2);
}

function originOf(url: string): string | null {
  try {
    return new URL(url).origin;
  } catch {
    return null;
  }
}

/** An admission Speakeasy minted when a device redeemed a bootstrap token. */
export function isDeviceAdmission(admission: WorkloadAdmission): boolean {
  return (
    admission.issuerName === DEVICE_ISSUER_NAME &&
    admission.subject.startsWith(DEVICE_SUBJECT_PREFIX)
  );
}

/**
 * A token this page created and is waiting on. The install command returns
 * the token's id; creating one by hand returns only its expiry, which is
 * stamped by the server and so identifies it in the list just as well.
 */
export type WatchedToken = { id: string } | { expiresAt: Date };

export function findWatchedToken(
  tokens: WorkloadDeviceJoinTokenSummary[],
  watched: WatchedToken,
): WorkloadDeviceJoinTokenSummary | undefined {
  return tokens.find((token) =>
    "id" in watched
      ? token.id === watched.id
      : // Both are written to the second, so compare at that precision.
        Math.floor(token.expiresAt.getTime() / 1000) ===
        Math.floor(watched.expiresAt.getTime() / 1000),
  );
}

/** The first watched token a device has redeemed, if any has been. */
export function redeemedToken(
  tokens: WorkloadDeviceJoinTokenSummary[],
  watched: WatchedToken[],
): WorkloadDeviceJoinTokenSummary | undefined {
  for (const entry of watched) {
    const token = findWatchedToken(tokens, entry);
    if (token?.status === "redeemed") return token;
  }
  return undefined;
}

/** The API origin a device must call: this dashboard's own server. */
export function controlPlaneURL(): string {
  return getServerURL() || window.location.origin;
}

/** "5m ago" or "in 45m": expiries are usually in the future. */
export function relativeTime(date: Date, now = Date.now()): string {
  const diff = date.getTime() - now;
  const minutes = Math.round(Math.abs(diff) / 60_000);
  const span =
    minutes < 1
      ? null
      : minutes < 60
        ? `${minutes}m`
        : minutes < 60 * 24
          ? `${Math.round(minutes / 60)}h`
          : `${Math.round(minutes / (60 * 24))}d`;
  if (span === null) return diff >= 0 ? "in under a minute" : "just now";
  return diff >= 0 ? `in ${span}` : `${span} ago`;
}

/** "14:32", or "2h 05m" past an hour. */
export function formatRemaining(ms: number): string {
  const total = Math.max(0, Math.floor(ms / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  if (hours > 0) return `${hours}h ${String(minutes).padStart(2, "0")}m`;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}
