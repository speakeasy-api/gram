import * as z from "zod/v4-mini";
import {
  ApproveShadowMCPApprovalRequestForm,
  ApproveShadowMCPApprovalRequestForm$Outbound,
} from "../components/approveshadowmcpapprovalrequestform.js";
export type ApproveShadowMCPApprovalRequestSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type ApproveShadowMCPApprovalRequestRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  approveShadowMCPApprovalRequestForm: ApproveShadowMCPApprovalRequestForm;
};
/** @internal */
export type ApproveShadowMCPApprovalRequestSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ApproveShadowMCPApprovalRequestSecurity$outboundSchema: z.ZodMiniType<
  ApproveShadowMCPApprovalRequestSecurity$Outbound,
  ApproveShadowMCPApprovalRequestSecurity
>;
export declare function approveShadowMCPApprovalRequestSecurityToJSON(
  approveShadowMCPApprovalRequestSecurity: ApproveShadowMCPApprovalRequestSecurity,
): string;
/** @internal */
export type ApproveShadowMCPApprovalRequestRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  ApproveShadowMCPApprovalRequestForm: ApproveShadowMCPApprovalRequestForm$Outbound;
};
/** @internal */
export declare const ApproveShadowMCPApprovalRequestRequest$outboundSchema: z.ZodMiniType<
  ApproveShadowMCPApprovalRequestRequest$Outbound,
  ApproveShadowMCPApprovalRequestRequest
>;
export declare function approveShadowMCPApprovalRequestRequestToJSON(
  approveShadowMCPApprovalRequestRequest: ApproveShadowMCPApprovalRequestRequest,
): string;
//# sourceMappingURL=approveshadowmcpapprovalrequest.d.ts.map
