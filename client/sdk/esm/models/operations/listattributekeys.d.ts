import * as z from "zod/v4-mini";
import {
  GetProjectMetricsSummaryPayload,
  GetProjectMetricsSummaryPayload$Outbound,
} from "../components/getprojectmetricssummarypayload.js";
export type ListAttributeKeysSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListAttributeKeysSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListAttributeKeysSecurity = {
  option1?: ListAttributeKeysSecurityOption1 | undefined;
  option2?: ListAttributeKeysSecurityOption2 | undefined;
};
export type ListAttributeKeysRequest = {
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
  getProjectMetricsSummaryPayload: GetProjectMetricsSummaryPayload;
};
/** @internal */
export type ListAttributeKeysSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListAttributeKeysSecurityOption1$outboundSchema: z.ZodMiniType<
  ListAttributeKeysSecurityOption1$Outbound,
  ListAttributeKeysSecurityOption1
>;
export declare function listAttributeKeysSecurityOption1ToJSON(
  listAttributeKeysSecurityOption1: ListAttributeKeysSecurityOption1,
): string;
/** @internal */
export type ListAttributeKeysSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListAttributeKeysSecurityOption2$outboundSchema: z.ZodMiniType<
  ListAttributeKeysSecurityOption2$Outbound,
  ListAttributeKeysSecurityOption2
>;
export declare function listAttributeKeysSecurityOption2ToJSON(
  listAttributeKeysSecurityOption2: ListAttributeKeysSecurityOption2,
): string;
/** @internal */
export type ListAttributeKeysSecurity$Outbound = {
  Option1?: ListAttributeKeysSecurityOption1$Outbound | undefined;
  Option2?: ListAttributeKeysSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListAttributeKeysSecurity$outboundSchema: z.ZodMiniType<
  ListAttributeKeysSecurity$Outbound,
  ListAttributeKeysSecurity
>;
export declare function listAttributeKeysSecurityToJSON(
  listAttributeKeysSecurity: ListAttributeKeysSecurity,
): string;
/** @internal */
export type ListAttributeKeysRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  GetProjectMetricsSummaryPayload: GetProjectMetricsSummaryPayload$Outbound;
};
/** @internal */
export declare const ListAttributeKeysRequest$outboundSchema: z.ZodMiniType<
  ListAttributeKeysRequest$Outbound,
  ListAttributeKeysRequest
>;
export declare function listAttributeKeysRequestToJSON(
  listAttributeKeysRequest: ListAttributeKeysRequest,
): string;
//# sourceMappingURL=listattributekeys.d.ts.map
