import {
  gramAdminFetch,
  gramAdminMutation,
  GramAdminError,
  errorMessage,
} from "@/lib/gramAdminApi";
export type RefreshChange = { name: string; action: string; reason?: string };
export type RefreshPlan = {
  id: string;
  sourceIdentity: string;
  sourceRevision: string;
  expiresAt: string;
  changes: RefreshChange[];
};
export type RefreshReceipt = {
  planId: string;
  sourceRevision: string;
  state: string;
  completedAt: string;
  changes: RefreshChange[];
};
export type RefreshStatus = {
  sourceIdentity: string;
  lastFullSuccessAt?: string;
  lastFullSuccessRevision?: string;
  lastAttemptRevision?: string;
  lastAttemptState?: string;
  conflicts?: RefreshChange[];
  failure?: string;
};
const path = "/admin/catalog.refresh";
const body = (request: { action: string; planId?: string }): RequestInit => ({
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(request),
});
export const refreshStatus = (): Promise<RefreshStatus> =>
  gramAdminFetch<RefreshStatus>(path, body({ action: "status" }));
export const previewRefresh = (): Promise<RefreshPlan> =>
  gramAdminMutation<RefreshPlan>(path, body({ action: "preview" }));
export const applyRefresh = (planId: string): Promise<RefreshReceipt> =>
  gramAdminMutation<RefreshReceipt>(path, body({ action: "apply", planId }));
export function refreshError(error: unknown): string {
  if (error instanceof GramAdminError) {
    if (error.status === 409 || error.status === 404)
      return "Plan is stale or unavailable. Preview again and confirm the new plan.";
    if (error.status === 503)
      return "Production source is unavailable or not configured. Ask an operator to configure the trusted catalog source and destination asset storage, then check status again.";
    if (error.status === 403)
      return "Sync is unavailable in this deployment or for this session. Use an authorized staff session in local or dev; production destinations are blocked.";
    if (error.status === 401)
      return "Your staff session expired. Sign in again, check status, and preview again before applying.";
  }
  return `${errorMessage(error)}. Check status and preview again before applying; the request may have completed. No apply retry was attempted.`;
}
