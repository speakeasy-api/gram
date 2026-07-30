import * as z from "zod/v4-mini";
export type ListTeamInvitesSecurity = {
  sessionHeaderGramSession?: string | undefined;
};
export type ListTeamInvitesRequest = {
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
export type ListTeamInvitesSecurity$Outbound = {
  "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListTeamInvitesSecurity$outboundSchema: z.ZodMiniType<
  ListTeamInvitesSecurity$Outbound,
  ListTeamInvitesSecurity
>;
export declare function listTeamInvitesSecurityToJSON(
  listTeamInvitesSecurity: ListTeamInvitesSecurity,
): string;
/** @internal */
export type ListTeamInvitesRequest$Outbound = {
  organization_id: string;
  "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListTeamInvitesRequest$outboundSchema: z.ZodMiniType<
  ListTeamInvitesRequest$Outbound,
  ListTeamInvitesRequest
>;
export declare function listTeamInvitesRequestToJSON(
  listTeamInvitesRequest: ListTeamInvitesRequest,
): string;
//# sourceMappingURL=listteaminvites.d.ts.map
