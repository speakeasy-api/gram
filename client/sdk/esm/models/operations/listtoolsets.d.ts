import * as z from "zod/v4-mini";
export type ListToolsetsSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListToolsetsSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListToolsetsSecurity = {
  option1?: ListToolsetsSecurityOption1 | undefined;
  option2?: ListToolsetsSecurityOption2 | undefined;
};
export type ListToolsetsRequest = {
  /**
   * Session header
   */
  gramSession?: string | undefined;
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * project header
   */
  gramProject?: string | undefined;
};
/** @internal */
export type ListToolsetsSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListToolsetsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListToolsetsSecurityOption1$Outbound,
  ListToolsetsSecurityOption1
>;
export declare function listToolsetsSecurityOption1ToJSON(
  listToolsetsSecurityOption1: ListToolsetsSecurityOption1,
): string;
/** @internal */
export type ListToolsetsSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListToolsetsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListToolsetsSecurityOption2$Outbound,
  ListToolsetsSecurityOption2
>;
export declare function listToolsetsSecurityOption2ToJSON(
  listToolsetsSecurityOption2: ListToolsetsSecurityOption2,
): string;
/** @internal */
export type ListToolsetsSecurity$Outbound = {
  Option1?: ListToolsetsSecurityOption1$Outbound | undefined;
  Option2?: ListToolsetsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListToolsetsSecurity$outboundSchema: z.ZodMiniType<
  ListToolsetsSecurity$Outbound,
  ListToolsetsSecurity
>;
export declare function listToolsetsSecurityToJSON(
  listToolsetsSecurity: ListToolsetsSecurity,
): string;
/** @internal */
export type ListToolsetsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListToolsetsRequest$outboundSchema: z.ZodMiniType<
  ListToolsetsRequest$Outbound,
  ListToolsetsRequest
>;
export declare function listToolsetsRequestToJSON(
  listToolsetsRequest: ListToolsetsRequest,
): string;
//# sourceMappingURL=listtoolsets.d.ts.map
