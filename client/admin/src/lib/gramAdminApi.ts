// Gram admin API client.
//
// This app is served from the same origin as the Gram admin API (the admin
// Ingress puts both behind one host), so every path below is relative
// and the `gram_admin` session cookie rides along as a first-party cookie.
//
// Do NOT reintroduce a configurable base URL. The pages used to live in the
// Registry admin app on a different registrable domain, which made
// `gram_admin` a third-party cookie. Chrome and Safari block those, so every
// credentialed fetch came back 401 and the app looped through login forever.
// SameSite=None permits a cross-site cookie; it does not defeat third-party
// cookie blocking. Same-origin is the fix.

export class GramAdminError extends Error {
  status: number;
  body: unknown;
  constructor(status: number, body: unknown, message: string) {
    super(message);
    this.status = status;
    this.body = body;
  }
}

// A 4xx body names what the operator has to fix, such as "at least one of
// account_type or whitelisted must be supplied". A 5xx body carries whatever
// verb phrase the handler passed to oops.E, such as "list organizations"
// (server/internal/admin/impl.go:343, surfaced by pp.go:83), which reads worse
// than the status line. So trust the body below 500 and nowhere else.
export function errorMessage(e: unknown): string {
  if (
    e instanceof GramAdminError &&
    e.status < 500 &&
    e.body &&
    typeof e.body === "object"
  ) {
    const message = (e.body as { message?: unknown }).message;
    if (typeof message === "string" && message) return message;
  }
  return e instanceof Error ? e.message : String(e);
}

export type QueryParams = Record<
  string,
  string | number | boolean | string[] | undefined
>;

// Values the admin API reads as unset. Every boolean it takes is an opt-in
// flag, so `false` is the same request as no flag at all.
//
// A cache key runs through this too, so the key and the request agree on what
// "unset" means. Without that, `{type: []}` and `{}` send one request and cache
// two entries.
export function omitUnset(params: QueryParams): QueryParams {
  return Object.fromEntries(
    Object.entries(params).filter(([, value]) => {
      if (Array.isArray(value)) return value.length > 0;
      return value !== undefined && value !== "" && value !== false;
    }),
  );
}

// An array becomes a repeated key (`type=free&type=pro`). Goa parses that into
// a slice; a comma-joined value would arrive as one string.
export function toSearchParams(params: QueryParams): URLSearchParams {
  const qs = new URLSearchParams();
  for (const [key, value] of Object.entries(omitUnset(params))) {
    if (Array.isArray(value)) {
      for (const item of value) qs.append(key, item);
    } else {
      qs.append(key, String(value));
    }
  }
  return qs;
}

async function gramAdminRequest(
  path: string,
  init: RequestInit | undefined,
  redirectOnUnauthorized: boolean,
): Promise<Response> {
  const url = path.startsWith("/") ? path : `/${path}`;
  // Accept is a default, not an override: a caller that sets its own wins.
  const headers = new Headers(init?.headers);
  if (!headers.has("Accept")) {
    headers.set("Accept", "application/json");
  }
  const res = await fetch(url, { ...init, headers });

  if (res.status === 401 && redirectOnUnauthorized) {
    // Top-level redirect into the OIDC flow. Use prompt=none so the identity
    // provider returns silently when the operator already has a session with
    // it. The gram admin backend falls back to interactive login if the
    // provider returns error=login_required (see Callback in
    // server/internal/admin/impl.go).
    //
    // return_to must stay a relative path. sanitizeReturnTo in
    // server/internal/admin/oauth.go keeps an absolute URL only when its origin
    // is listed in GRAM_ADMIN_ALLOWED_ORIGINS, which is empty by default, so an
    // absolute return_to silently loses the page the operator was on. The hash
    // is left out because the router keeps the whole route in the path and
    // query.
    const returnTo = encodeURIComponent(
      window.location.pathname + window.location.search,
    );
    redirectingToLogin = true;
    window.location.href = `/admin/auth.login?return_to=${returnTo}&prompt=none`;
    // Setting window.location starts the navigation but does not stop the code
    // that follows it. Throw to unwind the in-flight call.
    throw new GramAdminError(401, null, "redirecting to admin login");
  }

  if (!res.ok) {
    let body: unknown = null;
    try {
      body = await res.json();
    } catch {
      // ignore
    }
    throw new GramAdminError(
      res.status,
      body,
      `gram admin ${res.status} ${res.statusText}`,
    );
  }

  return res;
}

export async function gramAdminFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await gramAdminRequest(path, init, true);
  return (await res.json()) as T;
}

// For an endpoint that answers 204. A mutation reports its own failure rather
// than taking the 401 redirect, which would sign the operator back in behind
// the action they just took.
async function gramAdminSend(path: string, init?: RequestInit): Promise<void> {
  await gramAdminRequest(path, init, false);
}

// True once gramAdminFetch has sent the browser to the login page. The document
// is on its way out, so no caller should report the failure that caused it.
//
// The module records the navigation instead of reading it back off the failed
// query, because React Query clears the error of a query that holds no data on
// the next refetch, and a refetch on window focus would then reopen the gate
// while the browser is still leaving.
let redirectingToLogin = false;

export function isRedirectingToLogin(): boolean {
  return redirectingToLogin;
}

// Identity of the admin operator that owns the current session. The backend
// reads it from the OIDC session record, so it names the identity-provider
// account that signed in to this app, not any Gram customer account.
export type AdminSessionInfo = {
  email: string;
  name?: string;
};

export function getSession(): Promise<AdminSessionInfo> {
  return gramAdminFetch<AdminSessionInfo>("/admin/session.get");
}

// Ends the admin session, then sends the browser into the OIDC flow.
//
// The endpoint deletes only the server-side record and leaves the `gram_admin`
// cookie in the browser. The next request would therefore 401, and the 401
// handler retries with prompt=none, which the identity provider honours
// silently and signs the operator straight back in. Asking for select_account
// instead forces the account chooser, so logging out is visible.
export async function logout(): Promise<void> {
  await gramAdminSend("/admin/auth.logout", { method: "POST" });
  window.location.href = "/admin/auth.login?prompt=select_account";
}

// Convenience method for the listOrganizations endpoint. Mirrors the backend
// payload shape from server/gen/admin/service.go.
export type AdminOrganization = {
  id: string;
  name: string;
  slug: string;
  account_type: string;
  workos_id?: string;
  whitelisted: boolean;
  disabled_at?: string;
  free_trial_started_at?: string;
  free_trial_ends_at?: string;
  member_count: number;
  created_at: string;
  updated_at: string;
};

export type ListOrganizationsResult = {
  organizations: AdminOrganization[];
  next_cursor?: string;
};

export type ListOrganizationsParams = {
  q?: string;
  account_type?: string;
  include_disabled?: boolean;
  cursor?: string;
  limit?: number;
};

export function listOrganizations(
  params: ListOrganizationsParams = {},
): Promise<ListOrganizationsResult> {
  const qs = toSearchParams(params).toString();
  return gramAdminFetch<ListOrganizationsResult>(
    `/admin/organizations.list${qs ? `?${qs}` : ""}`,
  );
}

export type AdminProjectDetail = {
  id: string;
  name: string;
  slug: string;
  organization_id: string;
  logo_asset_id?: string;
  functions_runner_version?: string;
  toolset_count: number;
  deployment_count: number;
  http_tool_count: number;
  environment_count: number;
  api_key_count: number;
  assistant_count: number;
  created_at: string;
  updated_at: string;
};

export function getProject(idOrSlug: string): Promise<AdminProjectDetail> {
  const qs = toSearchParams({ id_or_slug: idOrSlug });
  return gramAdminFetch<AdminProjectDetail>(`/admin/project.get?${qs}`);
}

export function getOrganization(idOrSlug: string): Promise<AdminOrganization> {
  const qs = toSearchParams({ id_or_slug: idOrSlug });
  return gramAdminFetch<AdminOrganization>(`/admin/organization.get?${qs}`);
}

export type UpdateOrganizationRequest = {
  id: string;
  account_type?: string;
  whitelisted?: boolean;
};

export function updateOrganization(
  body: UpdateOrganizationRequest,
): Promise<AdminOrganization> {
  return gramAdminFetch<AdminOrganization>("/admin/organization.update", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export type AdminProject = {
  id: string;
  name: string;
  slug: string;
  created_at: string;
  updated_at: string;
};

export type ListOrganizationProjectsResult = {
  projects: AdminProject[];
};

export function listOrganizationProjects(
  organizationID: string,
): Promise<ListOrganizationProjectsResult> {
  const qs = toSearchParams({ organization_id: organizationID });
  return gramAdminFetch<ListOrganizationProjectsResult>(
    `/admin/organization.projects?${qs}`,
  );
}

export type AdminOrganizationMember = {
  id: string;
  email: string;
  display_name: string;
  last_login?: string;
  created_at: string;
  updated_at: string;
};

export type ListOrganizationMembersResult = {
  members: AdminOrganizationMember[];
};

export function listOrganizationMembers(
  organizationID: string,
): Promise<ListOrganizationMembersResult> {
  const qs = toSearchParams({ organization_id: organizationID });
  return gramAdminFetch<ListOrganizationMembersResult>(
    `/admin/organization.members?${qs}`,
  );
}
