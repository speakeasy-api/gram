import * as z from "zod/v4-mini";
import {
  CreateShadowMCPApprovalRequestForm,
  CreateShadowMCPApprovalRequestForm$Outbound,
} from "../components/createshadowmcpapprovalrequestform.js";
export type CreateShadowMCPApprovalRequestSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CreateShadowMCPApprovalRequestRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  createShadowMCPApprovalRequestForm: CreateShadowMCPApprovalRequestForm;
};
/** @internal */
export type CreateShadowMCPApprovalRequestSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateShadowMCPApprovalRequestSecurity$outboundSchema: z.ZodMiniType<
  CreateShadowMCPApprovalRequestSecurity$Outbound,
  CreateShadowMCPApprovalRequestSecurity
>;
export declare function createShadowMCPApprovalRequestSecurityToJSON(
  createShadowMCPApprovalRequestSecurity: CreateShadowMCPApprovalRequestSecurity,
): string;
/** @internal */
export type CreateShadowMCPApprovalRequestRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  CreateShadowMCPApprovalRequestForm: CreateShadowMCPApprovalRequestForm$Outbound;
};
/** @internal */
export declare const CreateShadowMCPApprovalRequestRequest$outboundSchema: z.ZodMiniType<
  CreateShadowMCPApprovalRequestRequest$Outbound,
  CreateShadowMCPApprovalRequestRequest
>;
export declare function createShadowMCPApprovalRequestRequestToJSON(
  createShadowMCPApprovalRequestRequest: CreateShadowMCPApprovalRequestRequest,
): string;
//# sourceMappingURL=createshadowmcpapprovalrequest.d.ts.map
