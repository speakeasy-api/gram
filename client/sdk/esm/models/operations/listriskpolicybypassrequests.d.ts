import * as z from "zod/v4-mini";
import { ClosedEnum } from "../../types/enums.js";
export type ListRiskPolicyBypassRequestsSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListRiskPolicyBypassRequestsSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListRiskPolicyBypassRequestsSecurity = {
  option1?: ListRiskPolicyBypassRequestsSecurityOption1 | undefined;
  option2?: ListRiskPolicyBypassRequestsSecurityOption2 | undefined;
};
/**
 * Optional request status filter.
 */
export declare const QueryParamStatus: {
  readonly Requested: "requested";
  readonly Approved: "approved";
  readonly Denied: "denied";
  readonly Revoked: "revoked";
};
/**
 * Optional request status filter.
 */
export type QueryParamStatus = ClosedEnum<typeof QueryParamStatus>;
export type ListRiskPolicyBypassRequestsRequest = {
  /**
   * Optional risk policy ID filter.
   */
  policyId?: string | undefined;
  /**
   * Optional request status filter.
   */
  status?: QueryParamStatus | undefined;
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
};
/** @internal */
export type ListRiskPolicyBypassRequestsSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRiskPolicyBypassRequestsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListRiskPolicyBypassRequestsSecurityOption1$Outbound,
  ListRiskPolicyBypassRequestsSecurityOption1
>;
export declare function listRiskPolicyBypassRequestsSecurityOption1ToJSON(
  listRiskPolicyBypassRequestsSecurityOption1: ListRiskPolicyBypassRequestsSecurityOption1,
): string;
/** @internal */
export type ListRiskPolicyBypassRequestsSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRiskPolicyBypassRequestsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListRiskPolicyBypassRequestsSecurityOption2$Outbound,
  ListRiskPolicyBypassRequestsSecurityOption2
>;
export declare function listRiskPolicyBypassRequestsSecurityOption2ToJSON(
  listRiskPolicyBypassRequestsSecurityOption2: ListRiskPolicyBypassRequestsSecurityOption2,
): string;
/** @internal */
export type ListRiskPolicyBypassRequestsSecurity$Outbound = {
  Option1?: ListRiskPolicyBypassRequestsSecurityOption1$Outbound | undefined;
  Option2?: ListRiskPolicyBypassRequestsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRiskPolicyBypassRequestsSecurity$outboundSchema: z.ZodMiniType<
  ListRiskPolicyBypassRequestsSecurity$Outbound,
  ListRiskPolicyBypassRequestsSecurity
>;
export declare function listRiskPolicyBypassRequestsSecurityToJSON(
  listRiskPolicyBypassRequestsSecurity: ListRiskPolicyBypassRequestsSecurity,
): string;
/** @internal */
export declare const QueryParamStatus$outboundSchema: z.ZodMiniEnum<
  typeof QueryParamStatus
>;
/** @internal */
export type ListRiskPolicyBypassRequestsRequest$Outbound = {
  policy_id?: string | undefined;
  status?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRiskPolicyBypassRequestsRequest$outboundSchema: z.ZodMiniType<
  ListRiskPolicyBypassRequestsRequest$Outbound,
  ListRiskPolicyBypassRequestsRequest
>;
export declare function listRiskPolicyBypassRequestsRequestToJSON(
  listRiskPolicyBypassRequestsRequest: ListRiskPolicyBypassRequestsRequest,
): string;
//# sourceMappingURL=listriskpolicybypassrequests.d.ts.map
