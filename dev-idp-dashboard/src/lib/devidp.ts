/**
 * The identity slots dev-idp keeps a currentUser for. "oauth2-1" holds a
 * local users-table row; "workos" holds a real WorkOS subject. Which one is
 * authoritative follows GRAM_DEVIDP_BACKEND.
 */
export type Mode = "oauth2-1" | "workos";

export const MODES: readonly Mode[] = ["oauth2-1", "workos"] as const;

/** The identity backend dev-idp is running against. */
export type Backend = "local" | "workos";

export interface User {
  id: string;
  email: string;
  display_name: string;
  admin: boolean;
  whitelisted: boolean;
  github_handle?: string;
  photo_url?: string;
  created_at: string;
  updated_at: string;
}

export interface Organization {
  id: string;
  name: string;
  slug: string;
  account_type: string;
  workos_id?: string;
  created_at: string;
  updated_at: string;
}

export interface Membership {
  id: string;
  user_id: string;
  organization_id: string;
  role: string;
  created_at: string;
  updated_at: string;
}

export interface WorkosCurrentUser {
  workos_sub: string;
  email?: string;
  first_name?: string;
  last_name?: string;
  organization_id?: string;
  profile_picture_url?: string;
}

export interface CurrentUser {
  mode: Mode;
  user?: User;
  workos?: WorkosCurrentUser;
}

/**
 * A requesting app: a client allowed to ask the IdP for an
 * ID-JAG on a user's behalf. An empty `client_secret` is a public client.
 */
export interface EmaApp {
  id: string;
  client_id: string;
  client_secret: string;
  /** JWKS document; when set the app authenticates with private_key_jwt. */
  jwks: string;
  name: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

/**
 * A resource app: one resource authorization server at /resource-as/<slug>.
 * `issuer` is what an ID-JAG must carry in `aud`; `resource_identifier` is
 * the MCP server behind it, which is what lands in the `resource` claim.
 * They are different URLs.
 */
export interface EmaResource {
  id: string;
  slug: string;
  name: string;
  resource_identifier: string;
  issuer: string;
  created_at: string;
  updated_at: string;
}

/** Which user may drive which app against which resource, and for what scopes. */
export interface EmaAppAssignment {
  id: string;
  app_id: string;
  user_id: string;
  resource_id: string;
  granted_scopes: string;
  created_at: string;
  updated_at: string;
}

/** Which issuer a resource authorization server accepts ID-JAGs from. */
export interface EmaTrustRule {
  id: string;
  resource_id: string;
  trusted_issuer: string;
  allowed_client_ids: string;
  allowed_scopes: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

/** One entry in the ledger of ID-JAGs the IdP has minted. */
export interface EmaIssuedJag {
  jti: string;
  app_id: string;
  user_id: string;
  resource_id: string;
  scope: string;
  expires_at: string;
  created_at: string;
}

export interface ListResult<T> {
  items: T[];
  next_cursor: string;
}

export interface ListParams {
  cursor?: string;
  limit?: number;
}

export interface ListMembershipsParams extends ListParams {
  user_id?: string;
  organization_id?: string;
}

export class RpcError extends Error {
  constructor(
    public readonly status: number,
    public readonly method: string,
    message: string,
  ) {
    super(message);
    this.name = "RpcError";
  }
}

async function rpc<TReq, TRes>(method: string, body: TReq): Promise<TRes> {
  const res = await fetch(`/rpc/${method}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new RpcError(res.status, method, text || res.statusText);
  }
  if (res.status === 204) return undefined as TRes;
  return (await res.json()) as TRes;
}

export const api = {
  organizations: {
    list: (p: ListParams = {}) =>
      rpc<ListParams, ListResult<Organization>>("organizations.list", p),
    create: (p: {
      name: string;
      slug: string;
      account_type?: string;
      workos_id?: string;
    }) => rpc<typeof p, Organization>("organizations.create", p),
    update: (p: {
      id: string;
      name?: string;
      slug?: string;
      account_type?: string;
      workos_id?: string;
    }) => rpc<typeof p, Organization>("organizations.update", p),
    delete: (p: { id: string }) =>
      rpc<typeof p, void>("organizations.delete", p),
  },
  users: {
    list: (p: ListParams = {}) =>
      rpc<ListParams, ListResult<User>>("users.list", p),
    create: (p: {
      email: string;
      display_name: string;
      admin?: boolean;
      whitelisted?: boolean;
      github_handle?: string;
      photo_url?: string;
    }) => rpc<typeof p, User>("users.create", p),
    update: (p: {
      id: string;
      email?: string;
      display_name?: string;
      admin?: boolean;
      whitelisted?: boolean;
      github_handle?: string;
      photo_url?: string;
    }) => rpc<typeof p, User>("users.update", p),
    delete: (p: { id: string }) => rpc<typeof p, void>("users.delete", p),
  },
  memberships: {
    list: (p: ListMembershipsParams = {}) =>
      rpc<ListMembershipsParams, ListResult<Membership>>("memberships.list", p),
    create: (p: { user_id: string; organization_id: string; role?: string }) =>
      rpc<typeof p, Membership>("memberships.create", p),
    update: (p: { id: string; role: string }) =>
      rpc<typeof p, Membership>("memberships.update", p),
    delete: (p: { id: string }) => rpc<typeof p, void>("memberships.delete", p),
  },
  devIdp: {
    getCurrentUser: (p: { mode: Mode }) =>
      rpc<typeof p, CurrentUser>("devIdp.getCurrentUser", p),
    setCurrentUser: (p: {
      mode: Mode;
      user_id?: string;
      workos_sub?: string;
    }) => rpc<typeof p, CurrentUser>("devIdp.setCurrentUser", p),
    clearCurrentUser: (p: { mode: Mode }) =>
      rpc<typeof p, void>("devIdp.clearCurrentUser", p),
  },
  emaApps: {
    list: (p: ListParams = {}) =>
      rpc<ListParams, ListResult<EmaApp>>("emaApps.list", p),
    create: (p: {
      client_id: string;
      client_secret?: string;
      jwks?: string;
      name?: string;
      enabled?: boolean;
    }) => rpc<typeof p, EmaApp>("emaApps.create", p),
    update: (p: {
      id: string;
      client_id?: string;
      client_secret?: string;
      jwks?: string;
      name?: string;
      enabled?: boolean;
    }) => rpc<typeof p, EmaApp>("emaApps.update", p),
    delete: (p: { id: string }) => rpc<typeof p, void>("emaApps.delete", p),
  },
  emaResources: {
    list: (p: ListParams = {}) =>
      rpc<ListParams, ListResult<EmaResource>>("emaResources.list", p),
    create: (p: { slug: string; name?: string; resource_identifier: string }) =>
      rpc<typeof p, EmaResource>("emaResources.create", p),
    update: (p: {
      id: string;
      slug?: string;
      name?: string;
      resource_identifier?: string;
    }) => rpc<typeof p, EmaResource>("emaResources.update", p),
    delete: (p: { id: string }) =>
      rpc<typeof p, void>("emaResources.delete", p),
  },
  emaAppAssignments: {
    list: (
      p: ListParams & {
        app_id?: string;
        user_id?: string;
        resource_id?: string;
      } = {},
    ) =>
      rpc<typeof p, ListResult<EmaAppAssignment>>("emaAppAssignments.list", p),
    create: (p: {
      app_id: string;
      user_id: string;
      resource_id: string;
      granted_scopes?: string;
    }) => rpc<typeof p, EmaAppAssignment>("emaAppAssignments.create", p),
    update: (p: { id: string; granted_scopes: string }) =>
      rpc<typeof p, EmaAppAssignment>("emaAppAssignments.update", p),
    delete: (p: { id: string }) =>
      rpc<typeof p, void>("emaAppAssignments.delete", p),
  },
  emaTrustRules: {
    list: (p: ListParams & { resource_id?: string } = {}) =>
      rpc<typeof p, ListResult<EmaTrustRule>>("emaTrustRules.list", p),
    create: (p: {
      resource_id: string;
      trusted_issuer: string;
      allowed_client_ids?: string;
      allowed_scopes?: string;
      enabled?: boolean;
    }) => rpc<typeof p, EmaTrustRule>("emaTrustRules.create", p),
    update: (p: {
      id: string;
      trusted_issuer?: string;
      allowed_client_ids?: string;
      allowed_scopes?: string;
      enabled?: boolean;
    }) => rpc<typeof p, EmaTrustRule>("emaTrustRules.update", p),
    delete: (p: { id: string }) =>
      rpc<typeof p, void>("emaTrustRules.delete", p),
    listIssuedGrants: (
      p: { user_id?: string; resource_id?: string; limit?: number } = {},
    ) =>
      rpc<typeof p, { items: EmaIssuedJag[] }>(
        "emaTrustRules.listIssuedGrants",
        p,
      ),
  },
};
