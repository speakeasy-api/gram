import { gramAdminFetch, gramAdminMutation } from "@/lib/gramAdminApi";
import {
  refreshError,
  type RefreshPlan,
  type RefreshReceipt,
  type RefreshStatus,
} from "./catalogRefresh";
export type ResetPlan = RefreshPlan & {
  operation: "reset";
  destination: string;
  confirmation: string;
  scopeWarning: string;
};
export type ResetReceipt = RefreshReceipt & {
  operation: "reset";
  destination: string;
};
export type ResetStatus = RefreshStatus & {
  lastAttemptOperation?: string;
  lastFullSuccessOperation?: string;
};
const path = "/admin/catalog.reset";
const body = (request: {
  action: string;
  planId?: string;
  confirmation?: string;
}): RequestInit => ({
  method: "POST",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify(request),
});
export const resetStatus = (): Promise<ResetStatus> =>
  gramAdminFetch<ResetStatus>(path, body({ action: "status" }));
export const previewReset = (): Promise<ResetPlan> =>
  gramAdminMutation<ResetPlan>(path, body({ action: "preview" }));
export const applyReset = ({
  planId,
  confirmation,
}: {
  planId: string;
  confirmation: string;
}): Promise<ResetReceipt> =>
  gramAdminMutation<ResetReceipt>(
    path,
    body({ action: "apply", planId, confirmation }),
  );
export const resetError = refreshError;
