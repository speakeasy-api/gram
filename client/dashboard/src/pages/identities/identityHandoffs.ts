import type { IdentityModel } from "@gram/client/models/components/identitymodel.js";
import { Operator } from "@gram/client/models/components/logfilter";
import type { useOrgRoutes, useRoutes } from "@/routes";
import { USER_EMAIL_FILTER_PATH } from "@/components/observe/observeTargetFilters";
import { serializeFilters } from "@/pages/logs/log-filter-url";

/**
 * The "Open in …" targets, pre-filtered to the person.
 *
 * Each subsystem filters on the identifier it records activity under, and each
 * page names that filter differently in the URL, so the mapping lives here
 * rather than being spelled out at every panel.
 */
export function identityHandoffs(
  identity: IdentityModel,
  routes: ReturnType<typeof useRoutes>,
  orgRoutes: ReturnType<typeof useOrgRoutes>,
  /** The member's principal URN, when the subject has a directory row. */
  principalUrn: string | undefined,
  /**
   * The window the reader has open, carried onto the destinations that read
   * the same params — landing on a different period than the panel showed
   * makes the handoff look like it filtered to the wrong person.
   */
  window: URLSearchParams = new URLSearchParams(),
): {
  auditLogs: string;
  agentSessions: string;
  toolLogs: string;
  costs: string;
  riskEvents: string;
  challenges: string;
  roles: string;
  shadowMcp: string;
  deviceAgent: string;
  mcpSessions: string;
} {
  const userId = identity.userIds[0];
  const email = identity.emails[0];
  const externalUserId = identity.externalUserIds[0];
  const query = (base: string, params: Record<string, string | undefined>) => {
    const search = new URLSearchParams();
    for (const key of ["range", "from", "to", "label"]) {
      const value = window.get(key);
      if (value) search.set(key, value);
    }
    for (const [key, value] of Object.entries(params)) {
      if (value) search.set(key, value);
    }
    const encoded = search.toString();
    return encoded ? `${base}?${encoded}` : base;
  };

  return {
    // Sessions filter on the subject URN the session store recorded, which
    // only exists for a subject with a directory row.
    mcpSessions: query(orgRoutes.mcpSessions.href(), {
      subjectUrn: userId ? `user:${userId}` : undefined,
      status: "active",
    }),
    // Audit logs key on the Gram user id, which is what its actor facet holds.
    auditLogs: query(orgRoutes.auditLogs.href(), { actor: userId }),
    // Agent sessions has no user dimension of its own; its search covers the
    // chat's user id and name, so the address is the closest honest filter.
    agentSessions: query(routes.agentSessions.href(), {
      search: email ?? externalUserId,
    }),
    // Tool Logs has no user param of its own: it reads its filters out of the
    // `af` chip list, so the person is expressed as an equality chip on the
    // same attribute path its own user filter writes.
    toolLogs: query(routes.logs.href(), {
      af: email
        ? (serializeFilters([
            {
              id: "identity-user-email",
              path: USER_EMAIL_FILTER_PATH,
              op: Operator.Eq,
              value: email,
            },
          ]) ?? undefined)
        : undefined,
    }),
    // Costs filters by drilling rather than by query param: the email
    // dimension is a path segment on the explorer.
    costs: query(
      email
        ? `${routes.costs.href()}/email~${encodeURIComponent(email)}`
        : routes.costs.href(),
      {},
    ),
    riskEvents: query(routes.riskEvents.href(), {
      user_id: externalUserId ?? email,
    }),
    challenges: query(`${orgRoutes.access.href()}/challenges`, {
      identity: principalUrn,
    }),
    // The roles panel continues on the roles tab, not the challenge log.
    roles: `${orgRoutes.access.href()}/roles`,
    // Neither of these has a per-person filter to carry: the shadow inventory
    // is keyed by server and the device list by device.
    shadowMcp: routes.shadowMCP.href(),
    deviceAgent: orgRoutes.deviceAgent.href(),
  };
}
