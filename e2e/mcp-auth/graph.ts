import { randomUUID } from "node:crypto";
import { execFile } from "node:child_process";

export type AuthGraphEdgeState = "present" | "missing" | "not_applicable";
export interface AuthGraphOffer {
  client: string;
  name?: string;
  type: string;
  source: string;
  enabled: boolean;
  deleted: boolean;
  bindingSource: string;
  bindingDeleted: boolean;
  registration: string | null;
  registrationDeleted: boolean | null;
  registrationScopePresent?: boolean | null;
  registrationResourcePresent?: boolean | null;
  issuerResourcePolicy?:
    | "not_applicable"
    | "unset"
    | "supported"
    | "unsupported";
  issuer: string | null;
  issuerDeleted: boolean | null;
  issuerRevocationEndpointPresent?: boolean | null;
  issuerBridge: AuthGraphEdgeState;
  directLegacyAssociation: AuthGraphEdgeState;
  selectedServerCount: number;
  selectedProjectCount: number;
  outsideSelectedServerCount: number;
}
export interface AuthGraphSelection {
  choice: string;
  credentialKind: string;
  noAuth: boolean;
  typedClient: string | null;
  missingTypedClient: boolean;
  registration: string | null;
  missingRegistration: boolean;
  source: string;
  deleted: boolean;
}
export interface AuthGraphGatewayMember {
  server: string;
  sortOrder: number;
  deleted: boolean;
}
export interface AuthGraphGateway {
  gateway: string;
  project: string;
  name: string;
  issuer: string | null;
  missingIssuer: boolean;
  deleted: boolean;
  members: AuthGraphGatewayMember[];
  outsideSelectedMemberCount: number;
}
export interface AuthGraphAutomaticGatewayRoles {
  project: string;
  issuerResolution: "sole" | "none" | "multiple";
  organizationGatewayIssuer: string | null;
  projectGatewayIssuer: string | null;
}
export interface AuthGraphAutomaticGateway {
  /** Graph signals only: headers, runtime authorization, and availability are not captured. */
  derivedOAuthSignal: boolean;
  legacyOAuthGate:
    | "matched"
    | "unmatched"
    | "issuer_unavailable"
    | "not_applicable";
  registrations: Array<{
    registration: string;
    liveLegacyAssociation: boolean;
    gatewayIssuerLink: AuthGraphEdgeState;
    organizationClientListEligible: boolean;
    registrationDeleted: boolean;
    providerDeleted: boolean | null;
  }>;
}
export interface AuthGraphServer {
  server: string;
  project: string;
  name: string | null;
  kind: string;
  authRequired: boolean | null;
  signInIssuer: string | null;
  derivedUpstreamIssuer: string | null;
  visibility: string;
  disabled: boolean;
  deleted: boolean;
  offers: AuthGraphOffer[];
  selections: AuthGraphSelection[];
  automaticGateway?: AuthGraphAutomaticGateway;
}
export interface AuthGraph {
  capturedAt: string;
  projects: string[];
  servers: AuthGraphServer[];
  gateways: AuthGraphGateway[];
  /** Computed gateways have no durable member rows in these tables. */
  automaticGateways: {
    organization: "not_captured";
    project: "not_captured";
    issuerLinks?: AuthGraphAutomaticGatewayRoles[];
  };
}

const failure = (code: string) => new Error(`AUTH_GRAPH_${code}`);
const literal = (value: string) => `'${value.replaceAll("'", "''")}'`;

/** Inspect UI-created local fixtures only. Never provisions or repairs graph state. */
export async function captureAuthGraph(
  projectURLs: string[],
  aliasSalt = randomUUID().replaceAll("-", ""),
): Promise<AuthGraph> {
  if (!/^[a-f0-9]{32}$/.test(aliasSalt)) throw failure("INVALID_ALIAS_SCOPE");
  if (projectURLs.length === 0 || projectURLs.length > 20)
    throw failure("INVALID_PROJECTS");
  const requested = projectURLs.map((value) => {
    try {
      const url = new URL(value);
      const parts = url.pathname.replace(/\/$/, "").split("/");
      const organization = parts[1];
      const slug = parts[3];
      if (
        !["http:", "https:"].includes(url.protocol) ||
        !["localhost", "127.0.0.1", "[::1]"].includes(url.hostname) ||
        url.username ||
        url.password ||
        url.search ||
        url.hash ||
        parts.length !== 4 ||
        parts[2] !== "projects" ||
        !organization ||
        !/^[a-zA-Z0-9_-]{1,100}$/.test(organization) ||
        !slug ||
        slug.length > 40 ||
        !/^auth-e2e-[a-z0-9-]+$/.test(slug)
      )
        throw failure("INVALID_PROJECTS");
      return { organization, slug };
    } catch {
      throw failure("INVALID_PROJECTS");
    }
  });
  const unique = [
    ...new Map(requested.map((p) => [JSON.stringify(p), p])).values(),
  ];
  let env: NodeJS.ProcessEnv;
  try {
    const url = new URL(process.env.GRAM_DATABASE_URL ?? "");
    if (
      !["postgres:", "postgresql:"].includes(url.protocol) ||
      !["localhost", "127.0.0.1", "[::1]"].includes(url.hostname) ||
      [...url.searchParams].some(
        ([key, value]) =>
          !(
            (key === "sslmode" && value === "disable") ||
            (key === "search_path" && ["public", "gram"].includes(value))
          ),
      ) ||
      url.hash ||
      !/^\/[^/]+$/.test(url.pathname)
    )
      throw failure("INVALID_DATABASE");
    const database = decodeURIComponent(url.pathname.slice(1));
    const user = decodeURIComponent(url.username);
    const password = decodeURIComponent(url.password);
    if (
      !user ||
      !database ||
      [database, user, password].some((v) => v.includes("\0"))
    )
      throw failure("INVALID_DATABASE");
    env = {
      PATH: process.env.PATH ?? "/usr/bin:/bin",
      LANG: "C",
      PGHOST: url.hostname === "[::1]" ? "::1" : url.hostname,
      PGPORT: url.port || "5432",
      PGDATABASE: database,
      PGUSER: user,
      PGPASSWORD: password,
      PGPASSFILE: "/dev/null",
      PGSERVICEFILE: "/dev/null",
      PGCONNECT_TIMEOUT: "3",
      PGSSLMODE: "disable",
      PGOPTIONS: `-c default_transaction_read_only=on -c search_path=${url.searchParams.get("search_path") ?? "public"},pg_catalog`,
    };
  } catch {
    throw failure("INVALID_DATABASE");
  }
  const values = unique
    .map((p) => `(${literal(p.organization)}, ${literal(p.slug)})`)
    .join(",");
  const sql = `
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL statement_timeout = '5s';
SET LOCAL lock_timeout = '1s';
SET LOCAL idle_in_transaction_session_timeout = '5s';
WITH requested(organization_slug, project_slug) AS (VALUES ${values}),
p AS (
  SELECT p.id, p.organization_id, 'project-' || substr(md5(p.id::text || '${aliasSalt}'), 1, 12) AS alias
  FROM projects p JOIN organization_metadata o ON o.id = p.organization_id
  JOIN requested q ON q.organization_slug = o.slug AND q.project_slug = p.slug
  WHERE NOT p.deleted
), s AS (
  SELECT m.*, p.organization_id, p.alias AS project_alias,
    'server-' || substr(md5(m.id::text || '${aliasSalt}'), 1, 12) AS alias
  FROM mcp_servers m JOIN p ON p.id = m.project_id
), edges AS (
  SELECT l.mcp_server_id, l.source AS binding_source, l.deleted AS binding_deleted,
    c.*, r.id AS registration_id, r.deleted AS registration_deleted,
    i.id AS issuer_id, i.deleted AS issuer_deleted,
    CASE WHEN i.id IS NOT NULL THEN nullif(i.revocation_endpoint, '') IS NOT NULL END AS issuer_revocation_endpoint_present,
    CASE WHEN r.id IS NOT NULL THEN coalesce(cardinality(r.scope), 0) > 0 END AS registration_scope_present,
    CASE WHEN r.id IS NOT NULL THEN nullif(r.resource_identifier, '') IS NOT NULL END AS registration_resource_present,
    CASE WHEN i.id IS NULL THEN 'not_applicable'
      WHEN i.resource_indicator_supported IS NULL THEN 'unset'
      WHEN i.resource_indicator_supported THEN 'supported' ELSE 'unsupported' END AS issuer_resource_policy,
    CASE WHEN r.id IS NULL THEN 'not_applicable'
      WHEN EXISTS (
        SELECT 1 FROM remote_session_client_user_session_issuers b
        JOIN user_session_issuers u ON u.id = b.user_session_issuer_id
          AND (u.organization_id = s.organization_id OR (u.organization_id IS NULL AND u.project_id IS NULL))
          AND (u.project_id IS NULL OR u.project_id = s.project_id) AND NOT u.deleted
        WHERE b.remote_session_client_id = r.id AND b.user_session_issuer_id = s.user_session_issuer_id
      ) THEN 'present' ELSE 'missing' END AS bridge,
    CASE WHEN r.id IS NULL THEN 'not_applicable'
      WHEN EXISTS (
        SELECT 1 FROM mcp_server_remote_session_clients d
        WHERE d.mcp_server_id = s.id AND d.project_id = s.project_id
          AND d.organization_id = s.organization_id AND d.remote_session_client_id = r.id AND NOT d.deleted
      ) THEN 'present' ELSE 'missing' END AS direct
  FROM s JOIN mcp_server_auth_clients l ON l.mcp_server_id = s.id
    AND l.project_id = s.project_id AND l.organization_id = s.organization_id
  JOIN mcp_auth_clients c ON c.id = l.auth_client_id AND c.organization_id = s.organization_id
    AND (c.project_id IS NULL OR c.project_id = s.project_id)
  LEFT JOIN remote_session_clients r ON r.id = c.remote_session_client_id
    AND (r.organization_id = s.organization_id OR (r.organization_id IS NULL AND r.project_id IS NULL))
    AND (r.project_id IS NULL OR r.project_id = s.project_id)
  LEFT JOIN remote_session_issuers i ON i.id = r.remote_session_issuer_id
    AND (i.organization_id = s.organization_id OR (i.organization_id IS NULL AND i.project_id IS NULL))
    AND (i.project_id IS NULL OR i.project_id = s.project_id)
), offers AS (
  SELECT e.*, 'client-' || substr(md5(e.id::text || '${aliasSalt}'), 1, 12) AS client_alias,
    CASE WHEN registration_id IS NOT NULL THEN 'registration-' || substr(md5(registration_id::text || '${aliasSalt}'), 1, 12) END AS registration_alias,
    CASE WHEN issuer_id IS NOT NULL THEN 'issuer-' || substr(md5(issuer_id::text || '${aliasSalt}'), 1, 12) END AS issuer_alias
  FROM edges e
), peers AS (
  -- Outside-project relations contribute counts only; never names or identifiers.
  SELECT c.id,
    count(DISTINCT m.id) FILTER (WHERE m.project_id IN (SELECT id FROM p))::int AS selected_servers,
    count(DISTINCT m.project_id) FILTER (WHERE m.project_id IN (SELECT id FROM p))::int AS selected_projects,
    count(DISTINCT m.id) FILTER (WHERE m.project_id NOT IN (SELECT id FROM p))::int AS outside_servers
  FROM (SELECT DISTINCT id, organization_id, project_id FROM edges WHERE NOT deleted) c
  JOIN mcp_server_auth_clients l ON l.auth_client_id = c.id AND l.organization_id = c.organization_id AND NOT l.deleted
  JOIN projects pp ON pp.id = l.project_id AND pp.organization_id = c.organization_id AND NOT pp.deleted
    AND (c.project_id IS NULL OR c.project_id = pp.id)
  JOIN mcp_servers m ON m.id = l.mcp_server_id AND m.project_id = pp.id AND NOT m.deleted
  GROUP BY c.id
), gateways AS (
  SELECT g.id, g.project_id, g.organization_id, g.name, g.deleted,
    p.alias AS project_alias,
    'gateway-' || substr(md5(g.id::text || '${aliasSalt}'), 1, 12) AS alias,
    g.user_session_issuer_id IS NOT NULL AND u.id IS NULL AS missing_issuer,
    CASE WHEN u.id IS NOT NULL THEN 'user-issuer-' || substr(md5(u.id::text || '${aliasSalt}'), 1, 12) END AS issuer_alias
  FROM meta_mcp_servers g JOIN p ON p.id = g.project_id AND p.organization_id = g.organization_id
  LEFT JOIN user_session_issuers u ON u.id = g.user_session_issuer_id AND NOT u.deleted
    AND (u.organization_id = g.organization_id OR (u.organization_id IS NULL AND u.project_id IS NULL))
    AND (u.project_id IS NULL OR u.project_id = g.project_id)
), gateway_members AS (
  SELECT g.id AS gateway_id, m.id, m.sort_order, m.deleted, selected.alias AS server_alias
  FROM gateways g JOIN meta_mcp_server_members m ON m.meta_mcp_server_id = g.id AND m.project_id = g.project_id
  JOIN mcp_servers target ON target.id = m.mcp_server_id AND target.project_id = m.project_id
  JOIN projects tp ON tp.id = target.project_id AND tp.organization_id = g.organization_id
  LEFT JOIN s selected ON selected.id = target.id AND selected.project_id = tp.id AND selected.organization_id = g.organization_id
), choices AS (
  SELECT q.id, q.mcp_server_id, q.credential_kind, q.no_auth, q.source, q.deleted,
    'choice-' || substr(md5(q.id::text || '${aliasSalt}'), 1, 12) AS alias,
    typed.client_alias,
    q.auth_client_id IS NOT NULL AND typed.client_alias IS NULL AS missing_typed_client,
    registration.registration_alias,
    q.remote_session_client_id IS NOT NULL AND registration.registration_alias IS NULL AS missing_registration
  FROM s JOIN upstream_client_selections q ON q.mcp_server_id = s.id
    AND q.project_id = s.project_id AND q.organization_id = s.organization_id
  -- Resolve only attached, live, tenant-bound offers already in this snapshot.
  -- A stale or unattached selection becomes a missing flag, never a raw ID.
  LEFT JOIN LATERAL (
    SELECT a.client_alias FROM offers a
    WHERE a.mcp_server_id = s.id AND a.id = q.auth_client_id
      AND a.organization_id = s.organization_id
      AND (a.project_id IS NULL OR a.project_id = s.project_id)
      AND NOT a.deleted AND NOT a.binding_deleted
    ORDER BY a.client_alias LIMIT 1
  ) typed ON true
  LEFT JOIN LATERAL (
    SELECT a.registration_alias FROM offers a
    WHERE a.mcp_server_id = s.id AND a.registration_id = q.remote_session_client_id
      AND a.organization_id = s.organization_id
      AND (a.project_id IS NULL OR a.project_id = s.project_id)
      AND NOT a.registration_deleted AND NOT a.deleted AND NOT a.binding_deleted
    ORDER BY a.registration_alias LIMIT 1
  ) registration ON true
), gateway_roles AS (
  -- Both automatic gateway scopes use the sole live organization-level issuer.
  SELECT p.id AS project_id, p.alias AS project_alias,
    count(u.id)::int AS issuer_count,
    CASE WHEN count(u.id) = 1 THEN (array_agg(u.id))[1] END AS issuer_id
  FROM p LEFT JOIN user_session_issuers u
    ON u.organization_id = p.organization_id AND u.project_id IS NULL AND NOT u.deleted
  GROUP BY p.id, p.alias
), gateway_role_aliases AS (
  SELECT g.*, CASE WHEN g.issuer_id IS NOT NULL THEN
    'user-issuer-' || substr(md5(g.issuer_id::text || '${aliasSalt}'), 1, 12) END AS issuer_alias
  FROM gateway_roles g
), gateway_legacy AS (
  SELECT DISTINCT s.id AS server_id, c.id AS registration_id
  FROM s JOIN mcp_server_remote_session_clients a
    ON a.mcp_server_id = s.id AND a.project_id = s.project_id
      AND a.organization_id = s.organization_id AND NOT a.deleted
  JOIN remote_session_clients c ON c.id = a.remote_session_client_id AND NOT c.deleted
    AND (c.project_id = s.project_id
      OR (c.project_id IS NULL AND c.organization_id = s.organization_id)
      OR (c.project_id IS NULL AND c.organization_id IS NULL))
), gateway_exact_registrations AS (
  SELECT mcp_server_id AS server_id, registration_id FROM offers
  WHERE registration_id IS NOT NULL
  UNION
  SELECT server_id, registration_id FROM gateway_legacy
), gateway_registration_edges AS (
  SELECT x.server_id, x.registration_id,
    'registration-' || substr(md5(x.registration_id::text || '${aliasSalt}'), 1, 12) AS registration_alias,
    EXISTS (SELECT 1 FROM gateway_legacy l
      WHERE l.server_id = x.server_id AND l.registration_id = x.registration_id) AS live_legacy_association,
    EXISTS (SELECT 1 FROM remote_session_client_user_session_issuers b
      WHERE b.remote_session_client_id = x.registration_id
        AND b.user_session_issuer_id = g.issuer_id) AS gateway_issuer_link,
    coalesce((NOT c.deleted AND i.id IS NOT NULL AND
      (c.project_id = s.project_id OR
        (c.project_id IS NULL AND c.organization_id = s.organization_id))), false) AS organization_client_list_eligible,
    c.deleted AS registration_deleted, i.deleted AS provider_deleted
  FROM gateway_exact_registrations x JOIN s ON s.id = x.server_id
  JOIN gateway_roles g ON g.project_id = s.project_id
  JOIN remote_session_clients c ON c.id = x.registration_id
  LEFT JOIN remote_session_issuers i ON i.id = c.remote_session_issuer_id
)
SELECT json_build_object(
  'capturedAt', transaction_timestamp(),
  'projects', (SELECT coalesce(json_agg(alias ORDER BY alias), '[]') FROM p),
  'automaticGateways', json_build_object('organization', 'not_captured', 'project', 'not_captured',
    'issuerLinks', (SELECT coalesce(json_agg(json_build_object(
      'project', g.project_alias,
      'issuerResolution', CASE g.issuer_count WHEN 0 THEN 'none' WHEN 1 THEN 'sole' ELSE 'multiple' END,
      'organizationGatewayIssuer', g.issuer_alias, 'projectGatewayIssuer', g.issuer_alias
    ) ORDER BY g.project_alias), '[]') FROM gateway_role_aliases g)),
  'gateways', (SELECT coalesce(json_agg(json_build_object(
    'gateway', g.alias, 'project', g.project_alias, 'name', g.name,
    'issuer', g.issuer_alias, 'missingIssuer', g.missing_issuer, 'deleted', g.deleted,
    'members', (SELECT coalesce(json_agg(json_build_object(
      'server', m.server_alias, 'sortOrder', m.sort_order, 'deleted', m.deleted
    ) ORDER BY m.sort_order, m.id), '[]') FROM gateway_members m
      WHERE m.gateway_id = g.id AND m.server_alias IS NOT NULL),
    'outsideSelectedMemberCount', (SELECT count(*)::int FROM gateway_members m
      WHERE m.gateway_id = g.id AND m.server_alias IS NULL)
  ) ORDER BY g.id), '[]') FROM gateways g),
  'servers', (SELECT coalesce(json_agg(json_build_object(
    'server', s.alias, 'project', s.project_alias, 'name', s.name,
    'kind', CASE WHEN s.remote_mcp_server_id IS NOT NULL THEN 'remote'
      WHEN s.tunneled_mcp_server_id IS NOT NULL THEN 'tunneled'
      WHEN s.toolset_id IS NOT NULL THEN 'toolset'
      WHEN s.unproxied_mcp_server_id IS NOT NULL THEN 'unproxied'
      WHEN s.builtin_backend IS NOT NULL THEN 'builtin' ELSE 'other' END,
    'authRequired', s.requires_upstream_auth,
    'signInIssuer', CASE WHEN s.user_session_issuer_id IS NOT NULL THEN 'user-issuer-' || substr(md5(s.user_session_issuer_id::text || '${aliasSalt}'), 1, 12) END,
    'derivedUpstreamIssuer', CASE WHEN s.remote_session_issuer_id IS NOT NULL THEN 'issuer-' || substr(md5(s.remote_session_issuer_id::text || '${aliasSalt}'), 1, 12) END,
    'visibility', s.visibility,
    'disabled', s.visibility = 'disabled', 'deleted', s.deleted,
    'automaticGateway', (SELECT json_build_object(
      'derivedOAuthSignal', s.remote_session_issuer_id IS NOT NULL OR
        EXISTS (SELECT 1 FROM gateway_legacy l WHERE l.server_id = s.id),
      -- This is only the exact legacy registration gate, not final eligibility.
      'legacyOAuthGate', CASE
        WHEN s.remote_mcp_server_id IS NULL AND s.tunneled_mcp_server_id IS NULL THEN 'not_applicable'
        WHEN g.issuer_count <> 1 THEN 'issuer_unavailable'
        WHEN EXISTS (SELECT 1 FROM gateway_registration_edges e WHERE e.server_id = s.id
          AND e.live_legacy_association AND e.gateway_issuer_link AND e.organization_client_list_eligible)
          THEN 'matched' ELSE 'unmatched' END,
      'registrations', (SELECT coalesce(json_agg(json_build_object(
        'registration', e.registration_alias,
        'liveLegacyAssociation', e.live_legacy_association,
        'gatewayIssuerLink', CASE WHEN g.issuer_count <> 1 THEN 'not_applicable'
          WHEN e.gateway_issuer_link THEN 'present' ELSE 'missing' END,
        'organizationClientListEligible', e.organization_client_list_eligible,
        'registrationDeleted', e.registration_deleted, 'providerDeleted', e.provider_deleted
      ) ORDER BY e.registration_alias), '[]') FROM gateway_registration_edges e WHERE e.server_id = s.id)
    ) FROM gateway_roles g WHERE g.project_id = s.project_id),
    'selections', (SELECT coalesce(json_agg(json_build_object(
      'choice', q.alias, 'credentialKind', q.credential_kind, 'noAuth', q.no_auth,
      'typedClient', q.client_alias, 'missingTypedClient', q.missing_typed_client,
      'registration', q.registration_alias, 'missingRegistration', q.missing_registration,
      'source', q.source, 'deleted', q.deleted
    ) ORDER BY q.id), '[]') FROM choices q WHERE q.mcp_server_id = s.id),
    'offers', (SELECT coalesce(json_agg(json_build_object(
      'client', a.client_alias, 'type', a.client_type, 'source', a.source,
      'name', a.name,
      'enabled', a.enabled, 'deleted', a.deleted,
      'bindingSource', a.binding_source, 'bindingDeleted', a.binding_deleted,
      'registration', a.registration_alias, 'registrationDeleted', a.registration_deleted,
      'registrationScopePresent', a.registration_scope_present,
      'registrationResourcePresent', a.registration_resource_present,
      'issuerResourcePolicy', a.issuer_resource_policy,
      'issuer', a.issuer_alias, 'issuerDeleted', a.issuer_deleted,
      'issuerRevocationEndpointPresent', a.issuer_revocation_endpoint_present,
      'issuerBridge', a.bridge, 'directLegacyAssociation', a.direct,
      'selectedServerCount', coalesce(peers.selected_servers, 0),
      'selectedProjectCount', coalesce(peers.selected_projects, 0),
      'outsideSelectedServerCount', coalesce(peers.outside_servers, 0)
    ) ORDER BY a.id, a.binding_deleted, a.binding_source), '[]')
    FROM offers a LEFT JOIN peers ON peers.id = a.id WHERE a.mcp_server_id = s.id)
  ) ORDER BY s.id), '[]') FROM s)
);
COMMIT;
`;
  const stdout = await new Promise<string>((resolve, reject) => {
    const child = execFile(
      "psql",
      ["-X", "-q", "-A", "-t", "-w", "-v", "ON_ERROR_STOP=1"],
      {
        env,
        encoding: "utf8",
        timeout: 15_000,
        maxBuffer: 1024 * 1024,
      },
      (error, output) => {
        // Never propagate child-process errors: they can contain stderr and credentials.
        if (error) reject(failure("QUERY_FAILED"));
        else resolve(output);
      },
    );
    child.stdin?.on("error", () => reject(failure("QUERY_FAILED")));
    child.stdin?.end(sql);
  }).catch(() => {
    throw failure("QUERY_FAILED");
  });
  try {
    const graph = JSON.parse(stdout) as AuthGraph;
    if (
      !Array.isArray(graph.projects) ||
      graph.projects.length !== unique.length ||
      !Array.isArray(graph.servers) ||
      !Array.isArray(graph.gateways) ||
      graph.automaticGateways?.organization !== "not_captured" ||
      graph.automaticGateways?.project !== "not_captured"
    )
      throw failure("INVALID_RESULT");
    return graph;
  } catch {
    throw failure("INVALID_RESULT");
  }
}
