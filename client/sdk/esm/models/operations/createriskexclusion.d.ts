import * as z from "zod/v4-mini";
import {
  CreateRiskExclusionRequestBody,
  CreateRiskExclusionRequestBody$Outbound,
} from "../components/createriskexclusionrequestbody.js";
export type CreateRiskExclusionSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type CreateRiskExclusionSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type CreateRiskExclusionSecurity = {
  option1?: CreateRiskExclusionSecurityOption1 | undefined;
  option2?: CreateRiskExclusionSecurityOption2 | undefined;
};
export type CreateRiskExclusionRequest = {
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
  createRiskExclusionRequestBody: CreateRiskExclusionRequestBody;
};
/** @internal */
export type CreateRiskExclusionSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CreateRiskExclusionSecurityOption1$outboundSchema: z.ZodMiniType<
  CreateRiskExclusionSecurityOption1$Outbound,
  CreateRiskExclusionSecurityOption1
>;
export declare function createRiskExclusionSecurityOption1ToJSON(
  createRiskExclusionSecurityOption1: CreateRiskExclusionSecurityOption1,
): string;
/** @internal */
export type CreateRiskExclusionSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const CreateRiskExclusionSecurityOption2$outboundSchema: z.ZodMiniType<
  CreateRiskExclusionSecurityOption2$Outbound,
  CreateRiskExclusionSecurityOption2
>;
export declare function createRiskExclusionSecurityOption2ToJSON(
  createRiskExclusionSecurityOption2: CreateRiskExclusionSecurityOption2,
): string;
/** @internal */
export type CreateRiskExclusionSecurity$Outbound = {
  Option1?: CreateRiskExclusionSecurityOption1$Outbound | undefined;
  Option2?: CreateRiskExclusionSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CreateRiskExclusionSecurity$outboundSchema: z.ZodMiniType<
  CreateRiskExclusionSecurity$Outbound,
  CreateRiskExclusionSecurity
>;
export declare function createRiskExclusionSecurityToJSON(
  createRiskExclusionSecurity: CreateRiskExclusionSecurity,
): string;
/** @internal */
export type CreateRiskExclusionRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  CreateRiskExclusionRequestBody: CreateRiskExclusionRequestBody$Outbound;
};
/** @internal */
export declare const CreateRiskExclusionRequest$outboundSchema: z.ZodMiniType<
  CreateRiskExclusionRequest$Outbound,
  CreateRiskExclusionRequest
>;
export declare function createRiskExclusionRequestToJSON(
  createRiskExclusionRequest: CreateRiskExclusionRequest,
): string;
//# sourceMappingURL=createriskexclusion.d.ts.map
