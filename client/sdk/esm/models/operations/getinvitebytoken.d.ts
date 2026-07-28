import * as z from "zod/v4-mini";
export type GetInviteByTokenRequest = {
  /**
   * Invitation token from the invite link.
   */
  token: string;
};
/** @internal */
export type GetInviteByTokenRequest$Outbound = {
  token: string;
};
/** @internal */
export declare const GetInviteByTokenRequest$outboundSchema: z.ZodMiniType<
  GetInviteByTokenRequest$Outbound,
  GetInviteByTokenRequest
>;
export declare function getInviteByTokenRequestToJSON(
  getInviteByTokenRequest: GetInviteByTokenRequest,
): string;
//# sourceMappingURL=getinvitebytoken.d.ts.map
