import * as z from "zod/v4-mini";
import {
  RiskPolicyBypassApprovalRequestBody,
  RiskPolicyBypassApprovalRequestBody$Outbound,
} from "../components/riskpolicybypassapprovalrequestbody.js";
export type ApproveRiskPolicyBypassRequestSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ApproveRiskPolicyBypassRequestSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ApproveRiskPolicyBypassRequestSecurity = {
  option1?: ApproveRiskPolicyBypassRequestSecurityOption1 | undefined;
  option2?: ApproveRiskPolicyBypassRequestSecurityOption2 | undefined;
};
export type ApproveRiskPolicyBypassRequestRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
  riskPolicyBypassApprovalRequestBody: RiskPolicyBypassApprovalRequestBody;
};
/** @internal */
export type ApproveRiskPolicyBypassRequestSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ApproveRiskPolicyBypassRequestSecurityOption1$outboundSchema: z.ZodMiniType<
  ApproveRiskPolicyBypassRequestSecurityOption1$Outbound,
  ApproveRiskPolicyBypassRequestSecurityOption1
>;
export declare function approveRiskPolicyBypassRequestSecurityOption1ToJSON(
  approveRiskPolicyBypassRequestSecurityOption1: ApproveRiskPolicyBypassRequestSecurityOption1,
): string;
/** @internal */
export type ApproveRiskPolicyBypassRequestSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ApproveRiskPolicyBypassRequestSecurityOption2$outboundSchema: z.ZodMiniType<
  ApproveRiskPolicyBypassRequestSecurityOption2$Outbound,
  ApproveRiskPolicyBypassRequestSecurityOption2
>;
export declare function approveRiskPolicyBypassRequestSecurityOption2ToJSON(
  approveRiskPolicyBypassRequestSecurityOption2: ApproveRiskPolicyBypassRequestSecurityOption2,
): string;
/** @internal */
export type ApproveRiskPolicyBypassRequestSecurity$Outbound = {
  Option1?: ApproveRiskPolicyBypassRequestSecurityOption1$Outbound | undefined;
  Option2?: ApproveRiskPolicyBypassRequestSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ApproveRiskPolicyBypassRequestSecurity$outboundSchema: z.ZodMiniType<
  ApproveRiskPolicyBypassRequestSecurity$Outbound,
  ApproveRiskPolicyBypassRequestSecurity
>;
export declare function approveRiskPolicyBypassRequestSecurityToJSON(
  approveRiskPolicyBypassRequestSecurity: ApproveRiskPolicyBypassRequestSecurity,
): string;
/** @internal */
export type ApproveRiskPolicyBypassRequestRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  RiskPolicyBypassApprovalRequestBody: RiskPolicyBypassApprovalRequestBody$Outbound;
};
/** @internal */
export declare const ApproveRiskPolicyBypassRequestRequest$outboundSchema: z.ZodMiniType<
  ApproveRiskPolicyBypassRequestRequest$Outbound,
  ApproveRiskPolicyBypassRequestRequest
>;
export declare function approveRiskPolicyBypassRequestRequestToJSON(
  approveRiskPolicyBypassRequestRequest: ApproveRiskPolicyBypassRequestRequest,
): string;
//# sourceMappingURL=approveriskpolicybypassrequest.d.ts.map
