import * as z from "zod/v4-mini";
import {
  GetEmployeeDataFlowGraphPayload,
  GetEmployeeDataFlowGraphPayload$Outbound,
} from "../components/getemployeedataflowgraphpayload.js";
export type GetEmployeeDataFlowGraphSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetEmployeeDataFlowGraphSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetEmployeeDataFlowGraphSecurity = {
  option1?: GetEmployeeDataFlowGraphSecurityOption1 | undefined;
  option2?: GetEmployeeDataFlowGraphSecurityOption2 | undefined;
};
export type GetEmployeeDataFlowGraphRequest = {
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
  getEmployeeDataFlowGraphPayload: GetEmployeeDataFlowGraphPayload;
};
/** @internal */
export type GetEmployeeDataFlowGraphSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetEmployeeDataFlowGraphSecurityOption1$outboundSchema: z.ZodMiniType<
  GetEmployeeDataFlowGraphSecurityOption1$Outbound,
  GetEmployeeDataFlowGraphSecurityOption1
>;
export declare function getEmployeeDataFlowGraphSecurityOption1ToJSON(
  getEmployeeDataFlowGraphSecurityOption1: GetEmployeeDataFlowGraphSecurityOption1,
): string;
/** @internal */
export type GetEmployeeDataFlowGraphSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetEmployeeDataFlowGraphSecurityOption2$outboundSchema: z.ZodMiniType<
  GetEmployeeDataFlowGraphSecurityOption2$Outbound,
  GetEmployeeDataFlowGraphSecurityOption2
>;
export declare function getEmployeeDataFlowGraphSecurityOption2ToJSON(
  getEmployeeDataFlowGraphSecurityOption2: GetEmployeeDataFlowGraphSecurityOption2,
): string;
/** @internal */
export type GetEmployeeDataFlowGraphSecurity$Outbound = {
  Option1?: GetEmployeeDataFlowGraphSecurityOption1$Outbound | undefined;
  Option2?: GetEmployeeDataFlowGraphSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetEmployeeDataFlowGraphSecurity$outboundSchema: z.ZodMiniType<
  GetEmployeeDataFlowGraphSecurity$Outbound,
  GetEmployeeDataFlowGraphSecurity
>;
export declare function getEmployeeDataFlowGraphSecurityToJSON(
  getEmployeeDataFlowGraphSecurity: GetEmployeeDataFlowGraphSecurity,
): string;
/** @internal */
export type GetEmployeeDataFlowGraphRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  GetEmployeeDataFlowGraphPayload: GetEmployeeDataFlowGraphPayload$Outbound;
};
/** @internal */
export declare const GetEmployeeDataFlowGraphRequest$outboundSchema: z.ZodMiniType<
  GetEmployeeDataFlowGraphRequest$Outbound,
  GetEmployeeDataFlowGraphRequest
>;
export declare function getEmployeeDataFlowGraphRequestToJSON(
  getEmployeeDataFlowGraphRequest: GetEmployeeDataFlowGraphRequest,
): string;
//# sourceMappingURL=getemployeedataflowgraph.d.ts.map
