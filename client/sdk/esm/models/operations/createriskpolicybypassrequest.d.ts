import * as z from "zod/v4-mini";
import {
  CreateShadowMCPApprovalRequestForm,
  CreateShadowMCPApprovalRequestForm$Outbound,
} from "../components/createshadowmcpapprovalrequestform.js";
export type CreateRiskPolicyBypassRequestSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type CreateRiskPolicyBypassRequestRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  createShadowMCPApprovalRequestForm: CreateShadowMCPApprovalRequestForm;
};
/** @internal */
export type CreateRiskPolicyBypassRequestSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateRiskPolicyBypassRequestSecurity$outboundSchema: z.ZodMiniType<
  CreateRiskPolicyBypassRequestSecurity$Outbound,
  CreateRiskPolicyBypassRequestSecurity
>;
export declare function createRiskPolicyBypassRequestSecurityToJSON(
  createRiskPolicyBypassRequestSecurity: CreateRiskPolicyBypassRequestSecurity,
): string;
/** @internal */
export type CreateRiskPolicyBypassRequestRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  CreateShadowMCPApprovalRequestForm: CreateShadowMCPApprovalRequestForm$Outbound;
};
/** @internal */
export declare const CreateRiskPolicyBypassRequestRequest$outboundSchema: z.ZodMiniType<
  CreateRiskPolicyBypassRequestRequest$Outbound,
  CreateRiskPolicyBypassRequestRequest
>;
export declare function createRiskPolicyBypassRequestRequestToJSON(
  createRiskPolicyBypassRequestRequest: CreateRiskPolicyBypassRequestRequest,
): string;
//# sourceMappingURL=createriskpolicybypassrequest.d.ts.map
