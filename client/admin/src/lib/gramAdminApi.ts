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

// gramAdminFetch is a thin wrapper around fetch that:
// - normalises the path to a root-relative URL,
// - redirects into the OIDC flow on 401,
// - throws on non-2xx with a typed error containing status + parsed body.
export async function gramAdminFetch<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const url = path.startsWith("/") ? path : `/${path}`;
  // Accept is a default, not an override: a caller that sets its own wins.
  const headers = new Headers(init?.headers);
  if (!headers.has("Accept")) {
    headers.set("Accept", "application/json");
  }
  const res = await fetch(url, { ...init, headers });

  if (res.status === 401) {
    // Top-level redirect into the OIDC flow. Use prompt=none so Google returns
    // silently when the user already has a Google session. The gram admin
    // backend falls back to interactive login if Google returns
    // error=login_required (see Callback in server/internal/admin/impl.go).
    const returnTo = encodeURIComponent(window.location.href);
    window.location.href = `/admin/auth.login?return_to=${returnTo}&prompt=none`;
    // Caller never sees a resolved value; throw to unwind the in-flight call.
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

  return (await res.json()) as T;
}

// Identity of the admin operator that owns the current session. The backend
// reads it from the OIDC session record, so it names the Google account that
// signed in to this app, not any Gram customer account.
export type AdminSessionInfo = {
  email: string;
  name?: string;
};

export function getSession(): Promise<AdminSessionInfo> {
  return gramAdminFetch<AdminSessionInfo>("/admin/session.get");
}

// Ends the admin session, then sends the browser into the OIDC flow.
//
// The endpoint answers 204, so this cannot use gramAdminFetch, which always
// parses a JSON body. The endpoint also deletes only the server-side record and
// leaves the `gram_admin` cookie in the browser. The next request would
// therefore 401, and the 401 handler retries with prompt=none, which Google
// honours silently and signs the operator straight back in. Asking for
// select_account instead forces the account chooser, so logging out is visible.
export async function logout(): Promise<void> {
  const res = await fetch("/admin/auth.logout", { method: "POST" });
  if (!res.ok) {
    throw new GramAdminError(
      res.status,
      null,
      `gram admin logout ${res.status} ${res.statusText}`,
    );
  }
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
  const qs = new URLSearchParams();
  if (params.q) qs.set("q", params.q);
  if (params.account_type) qs.set("account_type", params.account_type);
  if (params.include_disabled) qs.set("include_disabled", "true");
  if (params.cursor) qs.set("cursor", params.cursor);
  if (params.limit !== undefined) qs.set("limit", String(params.limit));
  const query = qs.toString();
  return gramAdminFetch<ListOrganizationsResult>(
    `/admin/organizations.list${query ? `?${query}` : ""}`,
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
  const qs = new URLSearchParams({ id_or_slug: idOrSlug });
  return gramAdminFetch<AdminProjectDetail>(`/admin/project.get?${qs}`);
}

export function getOrganization(idOrSlug: string): Promise<AdminOrganization> {
  const qs = new URLSearchParams({ id_or_slug: idOrSlug });
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
  const qs = new URLSearchParams({ organization_id: organizationID });
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
  const qs = new URLSearchParams({ organization_id: organizationID });
  return gramAdminFetch<ListOrganizationMembersResult>(
    `/admin/organization.members?${qs}`,
  );
}
