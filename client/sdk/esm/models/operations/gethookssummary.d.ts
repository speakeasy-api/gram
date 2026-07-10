import * as z from "zod/v4-mini";
import {
  GetHooksSummaryPayload,
  GetHooksSummaryPayload$Outbound,
} from "../components/gethookssummarypayload.js";
export type GetHooksSummarySecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type GetHooksSummarySecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type GetHooksSummarySecurity = {
  option1?: GetHooksSummarySecurityOption1 | undefined;
  option2?: GetHooksSummarySecurityOption2 | undefined;
};
export type GetHooksSummaryRequest = {
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
  getHooksSummaryPayload: GetHooksSummaryPayload;
};
/** @internal */
export type GetHooksSummarySecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const GetHooksSummarySecurityOption1$outboundSchema: z.ZodMiniType<
  GetHooksSummarySecurityOption1$Outbound,
  GetHooksSummarySecurityOption1
>;
export declare function getHooksSummarySecurityOption1ToJSON(
  getHooksSummarySecurityOption1: GetHooksSummarySecurityOption1,
): string;
/** @internal */
export type GetHooksSummarySecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const GetHooksSummarySecurityOption2$outboundSchema: z.ZodMiniType<
  GetHooksSummarySecurityOption2$Outbound,
  GetHooksSummarySecurityOption2
>;
export declare function getHooksSummarySecurityOption2ToJSON(
  getHooksSummarySecurityOption2: GetHooksSummarySecurityOption2,
): string;
/** @internal */
export type GetHooksSummarySecurity$Outbound = {
  Option1?: GetHooksSummarySecurityOption1$Outbound | undefined;
  Option2?: GetHooksSummarySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const GetHooksSummarySecurity$outboundSchema: z.ZodMiniType<
  GetHooksSummarySecurity$Outbound,
  GetHooksSummarySecurity
>;
export declare function getHooksSummarySecurityToJSON(
  getHooksSummarySecurity: GetHooksSummarySecurity,
): string;
/** @internal */
export type GetHooksSummaryRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  GetHooksSummaryPayload: GetHooksSummaryPayload$Outbound;
};
/** @internal */
export declare const GetHooksSummaryRequest$outboundSchema: z.ZodMiniType<
  GetHooksSummaryRequest$Outbound,
  GetHooksSummaryRequest
>;
export declare function getHooksSummaryRequestToJSON(
  getHooksSummaryRequest: GetHooksSummaryRequest,
): string;
//# sourceMappingURL=gethookssummary.d.ts.map
