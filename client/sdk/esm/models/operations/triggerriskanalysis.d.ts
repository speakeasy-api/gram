import * as z from "zod/v4-mini";
import {
  TriggerRiskAnalysisRequestBody,
  TriggerRiskAnalysisRequestBody$Outbound,
} from "../components/triggerriskanalysisrequestbody.js";
export type TriggerRiskAnalysisSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type TriggerRiskAnalysisSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type TriggerRiskAnalysisSecurity = {
  option1?: TriggerRiskAnalysisSecurityOption1 | undefined;
  option2?: TriggerRiskAnalysisSecurityOption2 | undefined;
};
export type TriggerRiskAnalysisRequest = {
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
  triggerRiskAnalysisRequestBody: TriggerRiskAnalysisRequestBody;
};
/** @internal */
export type TriggerRiskAnalysisSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const TriggerRiskAnalysisSecurityOption1$outboundSchema: z.ZodMiniType<
  TriggerRiskAnalysisSecurityOption1$Outbound,
  TriggerRiskAnalysisSecurityOption1
>;
export declare function triggerRiskAnalysisSecurityOption1ToJSON(
  triggerRiskAnalysisSecurityOption1: TriggerRiskAnalysisSecurityOption1,
): string;
/** @internal */
export type TriggerRiskAnalysisSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const TriggerRiskAnalysisSecurityOption2$outboundSchema: z.ZodMiniType<
  TriggerRiskAnalysisSecurityOption2$Outbound,
  TriggerRiskAnalysisSecurityOption2
>;
export declare function triggerRiskAnalysisSecurityOption2ToJSON(
  triggerRiskAnalysisSecurityOption2: TriggerRiskAnalysisSecurityOption2,
): string;
/** @internal */
export type TriggerRiskAnalysisSecurity$Outbound = {
  Option1?: TriggerRiskAnalysisSecurityOption1$Outbound | undefined;
  Option2?: TriggerRiskAnalysisSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const TriggerRiskAnalysisSecurity$outboundSchema: z.ZodMiniType<
  TriggerRiskAnalysisSecurity$Outbound,
  TriggerRiskAnalysisSecurity
>;
export declare function triggerRiskAnalysisSecurityToJSON(
  triggerRiskAnalysisSecurity: TriggerRiskAnalysisSecurity,
): string;
/** @internal */
export type TriggerRiskAnalysisRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  TriggerRiskAnalysisRequestBody: TriggerRiskAnalysisRequestBody$Outbound;
};
/** @internal */
export declare const TriggerRiskAnalysisRequest$outboundSchema: z.ZodMiniType<
  TriggerRiskAnalysisRequest$Outbound,
  TriggerRiskAnalysisRequest
>;
export declare function triggerRiskAnalysisRequestToJSON(
  triggerRiskAnalysisRequest: TriggerRiskAnalysisRequest,
): string;
//# sourceMappingURL=triggerriskanalysis.d.ts.map
