import * as z from "zod/v4-mini";
export type GetRiskCapabilitiesSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetRiskCapabilitiesSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetRiskCapabilitiesSecurity = {
  option1?: GetRiskCapabilitiesSecurityOption1 | undefined;
  option2?: GetRiskCapabilitiesSecurityOption2 | undefined;
};
export type GetRiskCapabilitiesRequest = {
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
export type GetRiskCapabilitiesSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetRiskCapabilitiesSecurityOption1$outboundSchema: z.ZodMiniType<
  GetRiskCapabilitiesSecurityOption1$Outbound,
  GetRiskCapabilitiesSecurityOption1
>;
export declare function getRiskCapabilitiesSecurityOption1ToJSON(
  getRiskCapabilitiesSecurityOption1: GetRiskCapabilitiesSecurityOption1,
): string;
/** @internal */
export type GetRiskCapabilitiesSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetRiskCapabilitiesSecurityOption2$outboundSchema: z.ZodMiniType<
  GetRiskCapabilitiesSecurityOption2$Outbound,
  GetRiskCapabilitiesSecurityOption2
>;
export declare function getRiskCapabilitiesSecurityOption2ToJSON(
  getRiskCapabilitiesSecurityOption2: GetRiskCapabilitiesSecurityOption2,
): string;
/** @internal */
export type GetRiskCapabilitiesSecurity$Outbound = {
  Option1?: GetRiskCapabilitiesSecurityOption1$Outbound | undefined;
  Option2?: GetRiskCapabilitiesSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetRiskCapabilitiesSecurity$outboundSchema: z.ZodMiniType<
  GetRiskCapabilitiesSecurity$Outbound,
  GetRiskCapabilitiesSecurity
>;
export declare function getRiskCapabilitiesSecurityToJSON(
  getRiskCapabilitiesSecurity: GetRiskCapabilitiesSecurity,
): string;
/** @internal */
export type GetRiskCapabilitiesRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetRiskCapabilitiesRequest$outboundSchema: z.ZodMiniType<
  GetRiskCapabilitiesRequest$Outbound,
  GetRiskCapabilitiesRequest
>;
export declare function getRiskCapabilitiesRequestToJSON(
  getRiskCapabilitiesRequest: GetRiskCapabilitiesRequest,
): string;
//# sourceMappingURL=getriskcapabilities.d.ts.map
