import * as z from "zod/v4-mini";
export type ListScopesSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ListScopesRequest = {
  /**
   * API Key header
   */
  gramKey?: string | undefined;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type ListScopesSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListScopesSecurity$outboundSchema: z.ZodMiniType<
  ListScopesSecurity$Outbound,
  ListScopesSecurity
>;
export declare function listScopesSecurityToJSON(
  listScopesSecurity: ListScopesSecurity,
): string;
/** @internal */
export type ListScopesRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListScopesRequest$outboundSchema: z.ZodMiniType<
  ListScopesRequest$Outbound,
  ListScopesRequest
>;
export declare function listScopesRequestToJSON(
  listScopesRequest: ListScopesRequest,
): string;
//# sourceMappingURL=listscopes.d.ts.map
