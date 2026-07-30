import * as z from "zod/v4-mini";
export type ListTeamMembersSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type ListTeamMembersRequest = {
  /**
   * The ID of the organization
   */
  organizationId: string;
  /**
   * Session header
   */
  gramSession?: string | undefined;
};
/** @internal */
export type ListTeamMembersSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListTeamMembersSecurity$outboundSchema: z.ZodMiniType<
  ListTeamMembersSecurity$Outbound,
  ListTeamMembersSecurity
>;
export declare function listTeamMembersSecurityToJSON(
  listTeamMembersSecurity: ListTeamMembersSecurity,
): string;
/** @internal */
export type ListTeamMembersRequest$Outbound = {
  organization_id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListTeamMembersRequest$outboundSchema: z.ZodMiniType<
  ListTeamMembersRequest$Outbound,
  ListTeamMembersRequest
>;
export declare function listTeamMembersRequestToJSON(
  listTeamMembersRequest: ListTeamMembersRequest,
): string;
//# sourceMappingURL=listteammembers.d.ts.map
