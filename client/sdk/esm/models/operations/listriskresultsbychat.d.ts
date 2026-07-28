import * as z from "zod/v4-mini";
export type ListRiskResultsByChatSecurityOption1 = {
  apikeyHeaderGramKey: string;
  projectSlugHeaderGramProject: string;
};
export type ListRiskResultsByChatSecurityOption2 = {
  projectSlugHeaderGramProject: string;
  sessionHeaderGramSession: string;
};
export type ListRiskResultsByChatSecurity = {
  option1?: ListRiskResultsByChatSecurityOption1 | undefined;
  option2?: ListRiskResultsByChatSecurityOption2 | undefined;
};
export type ListRiskResultsByChatRequest = {
  /**
   * Cursor to fetch the next page of results.
   */
  cursor?: string | undefined;
  /**
   * Maximum number of results to return per page.
   */
  limit?: number | undefined;
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
export type ListRiskResultsByChatSecurityOption1$Outbound = {
  "apikey_header_Gram-Key": string;
  "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const ListRiskResultsByChatSecurityOption1$outboundSchema: z.ZodMiniType<
  ListRiskResultsByChatSecurityOption1$Outbound,
  ListRiskResultsByChatSecurityOption1
>;
export declare function listRiskResultsByChatSecurityOption1ToJSON(
  listRiskResultsByChatSecurityOption1: ListRiskResultsByChatSecurityOption1,
): string;
/** @internal */
export type ListRiskResultsByChatSecurityOption2$Outbound = {
  "project_slug_header_Gram-Project": string;
  "session_header_Gram-Session": string;
};
/** @internal */
export declare const ListRiskResultsByChatSecurityOption2$outboundSchema: z.ZodMiniType<
  ListRiskResultsByChatSecurityOption2$Outbound,
  ListRiskResultsByChatSecurityOption2
>;
export declare function listRiskResultsByChatSecurityOption2ToJSON(
  listRiskResultsByChatSecurityOption2: ListRiskResultsByChatSecurityOption2,
): string;
/** @internal */
export type ListRiskResultsByChatSecurity$Outbound = {
  Option1?: ListRiskResultsByChatSecurityOption1$Outbound | undefined;
  Option2?: ListRiskResultsByChatSecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const ListRiskResultsByChatSecurity$outboundSchema: z.ZodMiniType<
  ListRiskResultsByChatSecurity$Outbound,
  ListRiskResultsByChatSecurity
>;
export declare function listRiskResultsByChatSecurityToJSON(
  listRiskResultsByChatSecurity: ListRiskResultsByChatSecurity,
): string;
/** @internal */
export type ListRiskResultsByChatRequest$Outbound = {
  cursor?: string | undefined;
  limit?: number | undefined;
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
  "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const ListRiskResultsByChatRequest$outboundSchema: z.ZodMiniType<
  ListRiskResultsByChatRequest$Outbound,
  ListRiskResultsByChatRequest
>;
export declare function listRiskResultsByChatRequestToJSON(
  listRiskResultsByChatRequest: ListRiskResultsByChatRequest,
): string;
//# sourceMappingURL=listriskresultsbychat.d.ts.map
