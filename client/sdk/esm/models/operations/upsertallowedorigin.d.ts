import * as z from "zod/v4-mini";
import {
  UpsertAllowedOriginForm,
  UpsertAllowedOriginForm$Outbound,
} from "../components/upsertallowedoriginform.js";
export type UpsertAllowedOriginSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type UpsertAllowedOriginSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type UpsertAllowedOriginSecurity = {
  option1?: UpsertAllowedOriginSecurityOption1 | undefined;
  option2?: UpsertAllowedOriginSecurityOption2 | undefined;
};
export type UpsertAllowedOriginRequest = {
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
  upsertAllowedOriginForm: UpsertAllowedOriginForm;
};
/** @internal */
export type UpsertAllowedOriginSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const UpsertAllowedOriginSecurityOption1$outboundSchema: z.ZodMiniType<
  UpsertAllowedOriginSecurityOption1$Outbound,
  UpsertAllowedOriginSecurityOption1
>;
export declare function upsertAllowedOriginSecurityOption1ToJSON(
  upsertAllowedOriginSecurityOption1: UpsertAllowedOriginSecurityOption1,
): string;
/** @internal */
export type UpsertAllowedOriginSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const UpsertAllowedOriginSecurityOption2$outboundSchema: z.ZodMiniType<
  UpsertAllowedOriginSecurityOption2$Outbound,
  UpsertAllowedOriginSecurityOption2
>;
export declare function upsertAllowedOriginSecurityOption2ToJSON(
  upsertAllowedOriginSecurityOption2: UpsertAllowedOriginSecurityOption2,
): string;
/** @internal */
export type UpsertAllowedOriginSecurity$Outbound = {
  Option1?: UpsertAllowedOriginSecurityOption1$Outbound | undefined;
  Option2?: UpsertAllowedOriginSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const UpsertAllowedOriginSecurity$outboundSchema: z.ZodMiniType<
  UpsertAllowedOriginSecurity$Outbound,
  UpsertAllowedOriginSecurity
>;
export declare function upsertAllowedOriginSecurityToJSON(
  upsertAllowedOriginSecurity: UpsertAllowedOriginSecurity,
): string;
/** @internal */
export type UpsertAllowedOriginRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
  UpsertAllowedOriginForm: UpsertAllowedOriginForm$Outbound;
};
/** @internal */
export declare const UpsertAllowedOriginRequest$outboundSchema: z.ZodMiniType<
  UpsertAllowedOriginRequest$Outbound,
  UpsertAllowedOriginRequest
>;
export declare function upsertAllowedOriginRequestToJSON(
  upsertAllowedOriginRequest: UpsertAllowedOriginRequest,
): string;
//# sourceMappingURL=upsertallowedorigin.d.ts.map
