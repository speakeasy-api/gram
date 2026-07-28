import * as z from "zod/v4-mini";
export type ListMembersSecurity = {
  apikeyHeaderGramKey?: string | undefined;
  sessionHeaderGramSession?: string | undefined;
};
export type ListMembersRequest = {
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
export type ListMembersSecurity$Outbound = {
  "apikey_header_Gram-Key"?: string | undefined;
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListMembersSecurity$outboundSchema: z.ZodMiniType<
  ListMembersSecurity$Outbound,
  ListMembersSecurity
>;
export declare function listMembersSecurityToJSON(
  listMembersSecurity: ListMembersSecurity,
): string;
/** @internal */
export type ListMembersRequest$Outbound = {
  "Gram-Key"?: string | undefined;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListMembersRequest$outboundSchema: z.ZodMiniType<
  ListMembersRequest$Outbound,
  ListMembersRequest
>;
export declare function listMembersRequestToJSON(
  listMembersRequest: ListMembersRequest,
): string;
//# sourceMappingURL=listmembers.d.ts.map
