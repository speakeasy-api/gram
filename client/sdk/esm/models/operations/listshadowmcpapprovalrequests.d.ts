import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListShadowMCPApprovalRequestsSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export declare const Status: {
  readonly Requested: "requested";
  readonly Approved: "approved";
  readonly Denied: "denied";
};
export type Status = ClosedEnum<typeof Status>;
export type ListShadowMCPApprovalRequestsRequest = {
  status?: Status | undefined;
  projectId?: string | undefined;
  limit?: number | undefined;
  /**
   * Cursor for the next page of results.
   */
  cursor?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type ListShadowMCPApprovalRequestsSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListShadowMCPApprovalRequestsSecurity$outboundSchema: z.ZodMiniType<
  ListShadowMCPApprovalRequestsSecurity$Outbound,
  ListShadowMCPApprovalRequestsSecurity
>;
export declare function listShadowMCPApprovalRequestsSecurityToJSON(
  listShadowMCPApprovalRequestsSecurity: ListShadowMCPApprovalRequestsSecurity,
): string;
/** @internal */
export declare const Status$outboundSchema: z.ZodMiniEnum<typeof Status>;
/** @internal */
export type ListShadowMCPApprovalRequestsRequest$Outbound = {
  status?: string | undefined;
  project_id?: string | undefined;
  limit: number;
  cursor?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListShadowMCPApprovalRequestsRequest$outboundSchema: z.ZodMiniType<
  ListShadowMCPApprovalRequestsRequest$Outbound,
  ListShadowMCPApprovalRequestsRequest
>;
export declare function listShadowMCPApprovalRequestsRequestToJSON(
  listShadowMCPApprovalRequestsRequest: ListShadowMCPApprovalRequestsRequest,
): string;
//# sourceMappingURL=listshadowmcpapprovalrequests.d.ts.map
