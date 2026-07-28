import * as z from "zod/v4-mini";
export type ListToolVariationGroupsSecurityOption1 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListToolVariationGroupsSecurityOption2 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListToolVariationGroupsSecurity = {
  option1?: ListToolVariationGroupsSecurityOption1 | undefined;
  option2?: ListToolVariationGroupsSecurityOption2 | undefined;
};
export type ListToolVariationGroupsRequest = {
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
export type ListToolVariationGroupsSecurityOption1$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListToolVariationGroupsSecurityOption1$outboundSchema: z.ZodMiniType<
  ListToolVariationGroupsSecurityOption1$Outbound,
  ListToolVariationGroupsSecurityOption1
>;
export declare function listToolVariationGroupsSecurityOption1ToJSON(
  listToolVariationGroupsSecurityOption1: ListToolVariationGroupsSecurityOption1,
): string;
/** @internal */
export type ListToolVariationGroupsSecurityOption2$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListToolVariationGroupsSecurityOption2$outboundSchema: z.ZodMiniType<
  ListToolVariationGroupsSecurityOption2$Outbound,
  ListToolVariationGroupsSecurityOption2
>;
export declare function listToolVariationGroupsSecurityOption2ToJSON(
  listToolVariationGroupsSecurityOption2: ListToolVariationGroupsSecurityOption2,
): string;
/** @internal */
export type ListToolVariationGroupsSecurity$Outbound = {
  Option1?: ListToolVariationGroupsSecurityOption1$Outbound | undefined;
  Option2?: ListToolVariationGroupsSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListToolVariationGroupsSecurity$outboundSchema: z.ZodMiniType<
  ListToolVariationGroupsSecurity$Outbound,
  ListToolVariationGroupsSecurity
>;
export declare function listToolVariationGroupsSecurityToJSON(
  listToolVariationGroupsSecurity: ListToolVariationGroupsSecurity,
): string;
/** @internal */
export type ListToolVariationGroupsRequest$Outbound = {
  "Gram-Session"?: string | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListToolVariationGroupsRequest$outboundSchema: z.ZodMiniType<
  ListToolVariationGroupsRequest$Outbound,
  ListToolVariationGroupsRequest
>;
export declare function listToolVariationGroupsRequestToJSON(
  listToolVariationGroupsRequest: ListToolVariationGroupsRequest,
): string;
//# sourceMappingURL=listtoolvariationgroups.d.ts.map
