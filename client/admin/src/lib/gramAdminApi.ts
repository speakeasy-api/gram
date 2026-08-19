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
    // Top-level redirect into the OIDC flow. Interactive consent ensures the
    // provider returns a refresh token; without one the one-hour access token
    // would send the operator through login again on every expiry.
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
    window.location.href = `/admin/auth.login?return_to=${returnTo}&prompt=consent`;
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

// A mutation reports its own failure rather than taking the 401 redirect,
// which would sign the operator back in behind the action they just took.
async function gramAdminMutation<T>(
  path: string,
  init?: RequestInit,
): Promise<T> {
  const res = await gramAdminRequest(path, init, false);
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

export function organizationDashboardUrl(organizationId: string): string {
  const query = new URLSearchParams({ organization_id: organizationId });
  return `/admin/organization.open-dashboard?${query.toString()}`;
}

// Ends the admin session, then sends the browser into the OIDC flow.
//
// The endpoint deletes only the server-side record and leaves the `gram_admin`
// cookie in the browser. Asking for select_account after deleting the session
// makes logout visible and lets the operator choose a different account.
export async function logout(): Promise<void> {
  await gramAdminSend("/admin/auth.logout", { method: "POST" });
  window.location.href = "/admin/auth.login?prompt=select_account";
}

// Derived server-side from the `trials` table, so it is the only trustworthy
// account of whether an organization ever trialled.
//
// A runtime list with the union derived from it, rather than a bare union: a
// test can then walk every state the server can send, so a seventh state added
// here fails a test as well as the build. A type annotation alone is one
// careless edit from being deleted, and nothing would notice.
export const TRIAL_STATES = [
  "none",
  "running",
  "ending_soon",
  "expired",
  "demoted",
  "converted",
] as const;

// Not `string`: a typo in a state name has to be a build failure, because
// every surface that reads it maps the state to a colour.
export type TrialState = (typeof TRIAL_STATES)[number];

// Convenience method for the listOrganizations endpoint. Mirrors the backend
// payload shape from server/gen/admin/service.go.
//
// `free_trial_started_at` and `free_trial_ends_at` are `NOT NULL` columns with
// a signup-plus-fourteen-days default that no application code writes, so they
// report a trial for every organization ever made. Nothing here reads them.
// They stay declared only because the API still sends them; a follow-up takes
// them off the wire.
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
  trial_state?: TrialState;
  trial_ends_at?: string;
  member_count: number;
  created_at: string;
  updated_at: string;
};

export type ListOrganizationsResult = {
  organizations: AdminOrganization[];
  next_cursor?: string;
};

// Each filter is a repeated parameter the server reads as a set, and an absent
// one means no filter of that kind: no account_types is every type, no
// trial_states is every state, no disabled_states is active organizations only.
//
// The scalar `account_type` and the `include_disabled` flag these replaced are
// still accepted by the server, so its half of this change can merge first.
// Nothing here sends them, and nothing should: two ways to say the same filter
// is how the browser and the server end up disagreeing about what is on.
export type ListOrganizationsParams = {
  q?: string;
  account_types?: string[];
  trial_states?: string[];
  disabled_states?: string[];
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

export type AdminOrganizationStats = {
  total: number;
  created_last_7_days: number;
  trials_ending_soon: number;
  disabled: number;
  disabled_last_7_days: number;
};

export function getOrganizationStats(): Promise<AdminOrganizationStats> {
  return gramAdminFetch<AdminOrganizationStats>("/admin/organizations.stats");
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

export function getProject(
  idOrSlug: string,
  organizationIdOrSlug?: string,
): Promise<AdminProjectDetail> {
  const qs = toSearchParams({
    id_or_slug: idOrSlug,
    organization_id_or_slug: organizationIdOrSlug,
  });
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

export type BulkUpdateAccountTypeRequest = {
  ids: string[];
  account_type: string;
};

// `updated_ids` is a set: the server states no order, so nothing may index into
// it or line it up against the request. `missing_ids` was not written, and a
// caller that drops it reports the write as having done more than it did.
export type BulkUpdateAccountTypeResult = {
  updated_ids: string[];
  missing_ids: string[];
};

// One statement for every id: an id that matches nothing comes back in
// missing_ids rather than failing the batch.
export function bulkUpdateAccountType(
  body: BulkUpdateAccountTypeRequest,
): Promise<BulkUpdateAccountTypeResult> {
  return gramAdminFetch<BulkUpdateAccountTypeResult>(
    "/admin/organizations.bulkUpdateAccountType",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    },
  );
}

export type OrganizationRequest = {
  id: string;
};

// Both answer the organization in its new state, so a caller updates its cache
// from the response rather than reading the record back.
export function disableOrganization(
  body: OrganizationRequest,
): Promise<AdminOrganization> {
  return gramAdminFetch<AdminOrganization>("/admin/organization.disable", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export function enableOrganization(
  body: OrganizationRequest,
): Promise<AdminOrganization> {
  return gramAdminFetch<AdminOrganization>("/admin/organization.enable", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

// The server's own bounds, mirrored so a value it would reject never leaves the
// browser. See MinTrialExtensionDays and MaxTrialExtensionDays in
// server/internal/constants/trials.go: zero moves nothing but updated_at, a
// negative shortens a trial through an endpoint named extend, and a year is
// where a trial becomes a contract.
export const MIN_TRIAL_EXTENSION_DAYS = 1;
export const MAX_TRIAL_EXTENSION_DAYS = 365;

export type ExtendTrialRequest = {
  id: string;
  days: number;
};

// The days are added to the trial's current end date, not to today, so an
// extension applied early does not shorten the trial.
export function extendTrial(
  body: ExtendTrialRequest,
): Promise<AdminOrganization> {
  return gramAdminFetch<AdminOrganization>("/admin/trial.extend", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

// The server's own bounds for a re-arm, mirrored the way the extension bounds
// above are. See MinTrialRearmDays and MaxTrialRearmDays in
// server/internal/constants/trials.go: separate names from the extension pair
// they alias today, so reading the extension bound here would follow the wrong
// one on the day they diverge.
export const MIN_TRIAL_REARM_DAYS = 1;
export const MAX_TRIAL_REARM_DAYS = 365;

export type RearmTrialRequest = {
  id: string;
  days: number;
};

// Not an extension with a different verb. The days are the whole length of a
// fresh run counted from now, and the write also restores the organization's
// account type and whitelist flag and revives its model provider keys. Only a
// demoted trial can be re-armed; anything else is refused with a conflict.
export function rearmTrial(
  body: RearmTrialRequest,
): Promise<AdminOrganization> {
  return gramAdminFetch<AdminOrganization>("/admin/trial.rearm", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export type CreateOrganizationRequest = {
  name: string;
};

export function createOrganization(
  body: CreateOrganizationRequest,
): Promise<AdminOrganization> {
  return gramAdminFetch<AdminOrganization>("/admin/organization.create", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export type AdminProject = {
  id: string;
  name: string;
  slug: string;
  // Both server models: every mcp_servers row, plus every mcp_enabled toolset
  // no such row points at. Required on the wire, so 0 is an answer rather than
  // an omission. AGE-3276.
  mcp_server_count: number;
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

export type AdminInferenceKey = {
  key_type: string;
  credits_used: number;
  monthly_credits: number;
  disabled: boolean;
};

export type AdminOrganizationFeatures = {
  authz_challenge_logging_enabled: boolean;
  customer_managed_encryption_keys_enabled: boolean;
  custom_model_keys_enabled: boolean;
  platform_mcp_enabled: boolean;
  remote_session_auto_refresh_enabled: boolean;
  sso_enabled: boolean;
  scim_enabled: boolean;
};

export type AdminOrganizationFeatureName =
  | "authz_challenge_logging"
  | "customer_managed_encryption_keys"
  | "custom_model_keys"
  | "platform_mcp"
  | "remote_session_auto_refresh"
  | "sso"
  | "scim";

export function getOrganizationFeatures(
  organizationID: string,
): Promise<AdminOrganizationFeatures> {
  const qs = toSearchParams({ organization_id: organizationID });
  return gramAdminFetch<AdminOrganizationFeatures>(
    `/admin/organization.features?${qs}`,
  );
}

export function setOrganizationFeature(input: {
  organizationID: string;
  featureName: AdminOrganizationFeatureName;
  enabled: boolean;
}): Promise<AdminOrganizationFeatures> {
  return gramAdminMutation<AdminOrganizationFeatures>(
    "/admin/organization.features",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        organization_id: input.organizationID,
        feature_name: input.featureName,
        enabled: input.enabled,
      }),
    },
  );
}

export type AdminChatAnalysisJudge = "work_units" | "business_memory";

export type AdminOrganizationChatAnalysisSettings = {
  organization_id: string;
  work_units_enabled: boolean;
  work_units_daily_cap: number;
  business_memory_enabled: boolean;
  business_memory_daily_cap: number;
  is_default: boolean;
};

export function getOrganizationChatAnalysisSettings(
  organizationID: string,
): Promise<AdminOrganizationChatAnalysisSettings> {
  const qs = toSearchParams({ organization_id: organizationID });
  return gramAdminFetch<AdminOrganizationChatAnalysisSettings>(
    `/admin/organization.chatAnalysisSettings?${qs}`,
  );
}

export type AdminChatAnalysisTriggerResult = {
  projects_signaled: number;
};

export function triggerOrganizationChatAnalysis(
  organizationID: string,
): Promise<AdminChatAnalysisTriggerResult> {
  return gramAdminMutation<AdminChatAnalysisTriggerResult>(
    "/admin/organization.chatAnalysisTrigger",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ organization_id: organizationID }),
    },
  );
}

export function setOrganizationChatAnalysisSetting(input: {
  organizationID: string;
  judge: AdminChatAnalysisJudge;
  enabled: boolean;
  dailyCap: number;
}): Promise<AdminOrganizationChatAnalysisSettings> {
  return gramAdminMutation<AdminOrganizationChatAnalysisSettings>(
    "/admin/organization.chatAnalysisSettings",
    {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        organization_id: input.organizationID,
        judge: input.judge,
        enabled: input.enabled,
        daily_cap: input.dailyCap,
      }),
    },
  );
}

export function getInferenceKeys(
  organizationID: string,
): Promise<AdminInferenceKey[]> {
  const qs = toSearchParams({ organization_id: organizationID });
  return gramAdminFetch<AdminInferenceKey[]>(
    `/admin/organization.inferenceKeys?${qs}`,
  );
}

export type AdminPaygBillingSummary = {
  period_start: string;
  period_end: string;
  tum_tokens: number;
  tum_unit_price_usd: string;
  tum_cost_usd: string;
  other_inference_spend_usd: string;
  recorded_through?: string;
  estimated_total_usd: string;
};

export type AdminStripeSubscriptionStatus =
  | "incomplete"
  | "incomplete_expired"
  | "trialing"
  | "active"
  | "past_due"
  | "canceled"
  | "unpaid"
  | "paused";

export type AdminStripeSubscription = {
  status: AdminStripeSubscriptionStatus;
  current_period_start: string;
  current_period_end: string;
  trial_start?: string;
  trial_end?: string;
  cancel_at_period_end: boolean;
  cancel_at?: string;
  canceled_at?: string;
  payment_failed: boolean;
};

export function getPaygBillingSummary(
  organizationID: string,
): Promise<AdminPaygBillingSummary> {
  const qs = toSearchParams({ organization_id: organizationID });
  return gramAdminFetch<AdminPaygBillingSummary>(
    `/admin/organization.paygBillingSummary?${qs}`,
  );
}

export function getStripeSubscription(
  organizationID: string,
): Promise<AdminStripeSubscription> {
  const qs = toSearchParams({ organization_id: organizationID });
  return gramAdminFetch<AdminStripeSubscription>(
    `/admin/organization.stripeSubscription?${qs}`,
  );
}

function updateStripeSubscription(
  path: string,
  organizationID: string,
): Promise<AdminStripeSubscription> {
  return gramAdminMutation<AdminStripeSubscription>(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ organization_id: organizationID }),
  });
}

export function cancelStripeSubscription(
  organizationID: string,
): Promise<AdminStripeSubscription> {
  return updateStripeSubscription(
    "/admin/organization.cancelStripeSubscription",
    organizationID,
  );
}

export function resumeStripeSubscription(
  organizationID: string,
): Promise<AdminStripeSubscription> {
  return updateStripeSubscription(
    "/admin/organization.resumeStripeSubscription",
    organizationID,
  );
}
