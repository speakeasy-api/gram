import * as z from "zod/v4-mini";
export type ListBuiltinExclusionsSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListBuiltinExclusionsSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListBuiltinExclusionsSecurity = {
  option1?: ListBuiltinExclusionsSecurityOption1 | undefined;
  option2?: ListBuiltinExclusionsSecurityOption2 | undefined;
};
export type ListBuiltinExclusionsRequest = {
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
export type ListBuiltinExclusionsSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListBuiltinExclusionsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListBuiltinExclusionsSecurityOption1$Outbound,
  ListBuiltinExclusionsSecurityOption1
>;
export declare function listBuiltinExclusionsSecurityOption1ToJSON(
  listBuiltinExclusionsSecurityOption1: ListBuiltinExclusionsSecurityOption1,
): string;
/** @internal */
export type ListBuiltinExclusionsSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListBuiltinExclusionsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListBuiltinExclusionsSecurityOption2$Outbound,
  ListBuiltinExclusionsSecurityOption2
>;
export declare function listBuiltinExclusionsSecurityOption2ToJSON(
  listBuiltinExclusionsSecurityOption2: ListBuiltinExclusionsSecurityOption2,
): string;
/** @internal */
export type ListBuiltinExclusionsSecurity$Outbound = {
  Option1?: ListBuiltinExclusionsSecurityOption1$Outbound | undefined;
  Option2?: ListBuiltinExclusionsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListBuiltinExclusionsSecurity$outboundSchema: z.ZodMiniType<
  ListBuiltinExclusionsSecurity$Outbound,
  ListBuiltinExclusionsSecurity
>;
export declare function listBuiltinExclusionsSecurityToJSON(
  listBuiltinExclusionsSecurity: ListBuiltinExclusionsSecurity,
): string;
/** @internal */
export type ListBuiltinExclusionsRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListBuiltinExclusionsRequest$outboundSchema: z.ZodMiniType<
  ListBuiltinExclusionsRequest$Outbound,
  ListBuiltinExclusionsRequest
>;
export declare function listBuiltinExclusionsRequestToJSON(
  listBuiltinExclusionsRequest: ListBuiltinExclusionsRequest,
): string;
//# sourceMappingURL=listbuiltinexclusions.d.ts.map
