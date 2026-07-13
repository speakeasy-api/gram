import * as z from "zod/v4-mini";
import {
  GetObservabilityOverviewPayload,
  GetObservabilityOverviewPayload$Outbound,
} from "../components/getobservabilityoverviewpayload.js";
export type GetObservabilityOverviewSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetObservabilityOverviewSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetObservabilityOverviewSecurity = {
  option1?: GetObservabilityOverviewSecurityOption1 | undefined;
  option2?: GetObservabilityOverviewSecurityOption2 | undefined;
};
export type GetObservabilityOverviewRequest = {
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
  getObservabilityOverviewPayload: GetObservabilityOverviewPayload;
};
/** @internal */
export type GetObservabilityOverviewSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetObservabilityOverviewSecurityOption1$outboundSchema: z.ZodMiniType<
  GetObservabilityOverviewSecurityOption1$Outbound,
  GetObservabilityOverviewSecurityOption1
>;
export declare function getObservabilityOverviewSecurityOption1ToJSON(
  getObservabilityOverviewSecurityOption1: GetObservabilityOverviewSecurityOption1,
): string;
/** @internal */
export type GetObservabilityOverviewSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetObservabilityOverviewSecurityOption2$outboundSchema: z.ZodMiniType<
  GetObservabilityOverviewSecurityOption2$Outbound,
  GetObservabilityOverviewSecurityOption2
>;
export declare function getObservabilityOverviewSecurityOption2ToJSON(
  getObservabilityOverviewSecurityOption2: GetObservabilityOverviewSecurityOption2,
): string;
/** @internal */
export type GetObservabilityOverviewSecurity$Outbound = {
  Option1?: GetObservabilityOverviewSecurityOption1$Outbound | undefined;
  Option2?: GetObservabilityOverviewSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetObservabilityOverviewSecurity$outboundSchema: z.ZodMiniType<
  GetObservabilityOverviewSecurity$Outbound,
  GetObservabilityOverviewSecurity
>;
export declare function getObservabilityOverviewSecurityToJSON(
  getObservabilityOverviewSecurity: GetObservabilityOverviewSecurity,
): string;
/** @internal */
export type GetObservabilityOverviewRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  GetObservabilityOverviewPayload: GetObservabilityOverviewPayload$Outbound;
};
/** @internal */
export declare const GetObservabilityOverviewRequest$outboundSchema: z.ZodMiniType<
  GetObservabilityOverviewRequest$Outbound,
  GetObservabilityOverviewRequest
>;
export declare function getObservabilityOverviewRequestToJSON(
  getObservabilityOverviewRequest: GetObservabilityOverviewRequest,
): string;
//# sourceMappingURL=getobservabilityoverview.d.ts.map
