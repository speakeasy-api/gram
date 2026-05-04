export type Mode = "mock-speakeasy" | "oauth2-1" | "oauth2" | "workos";

export const MODES: readonly Mode[] = [
  "mock-speakeasy",
  "oauth2-1",
  "oauth2",
  "workos",
] as const;

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
  },
};
