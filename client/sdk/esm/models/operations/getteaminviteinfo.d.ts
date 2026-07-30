import * as z from "zod/v4-mini";
export type GetTeamInviteInfoRequest = {
  /**
   * The invite token from the email link
   */
  token: string;
};
/** @internal */
export type GetTeamInviteInfoRequest$Outbound = {
  token: string;
};
/** @internal */
export declare const GetTeamInviteInfoRequest$outboundSchema: z.ZodMiniType<
  GetTeamInviteInfoRequest$Outbound,
  GetTeamInviteInfoRequest
>;
export declare function getTeamInviteInfoRequestToJSON(
  getTeamInviteInfoRequest: GetTeamInviteInfoRequest,
): string;
//# sourceMappingURL=getteaminviteinfo.d.ts.map
