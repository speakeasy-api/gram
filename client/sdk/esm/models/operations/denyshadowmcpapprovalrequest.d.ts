import * as z from "zod/v4-mini";
import {
  DenyShadowMCPApprovalRequestForm,
  DenyShadowMCPApprovalRequestForm$Outbound,
} from "../components/denyshadowmcpapprovalrequestform.js";
export type DenyShadowMCPApprovalRequestSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type DenyShadowMCPApprovalRequestRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  denyShadowMCPApprovalRequestForm: DenyShadowMCPApprovalRequestForm;
};
/** @internal */
export type DenyShadowMCPApprovalRequestSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DenyShadowMCPApprovalRequestSecurity$outboundSchema: z.ZodMiniType<
  DenyShadowMCPApprovalRequestSecurity$Outbound,
  DenyShadowMCPApprovalRequestSecurity
>;
export declare function denyShadowMCPApprovalRequestSecurityToJSON(
  denyShadowMCPApprovalRequestSecurity: DenyShadowMCPApprovalRequestSecurity,
): string;
/** @internal */
export type DenyShadowMCPApprovalRequestRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  DenyShadowMCPApprovalRequestForm: DenyShadowMCPApprovalRequestForm$Outbound;
};
/** @internal */
export declare const DenyShadowMCPApprovalRequestRequest$outboundSchema: z.ZodMiniType<
  DenyShadowMCPApprovalRequestRequest$Outbound,
  DenyShadowMCPApprovalRequestRequest
>;
export declare function denyShadowMCPApprovalRequestRequestToJSON(
  denyShadowMCPApprovalRequestRequest: DenyShadowMCPApprovalRequestRequest,
): string;
//# sourceMappingURL=denyshadowmcpapprovalrequest.d.ts.map
