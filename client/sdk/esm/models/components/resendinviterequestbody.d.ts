import * as z from "zod/v4-mini";
export type ResendInviteRequestBody = {
  /**
   * The ID of the invite to resend
   */
  inviteId: string;
};
/** @internal */
export type ResendInviteRequestBody$Outbound = {
  invite_id: string;
};
/** @internal */
export declare const ResendInviteRequestBody$outboundSchema: z.ZodMiniType<
  ResendInviteRequestBody$Outbound,
  ResendInviteRequestBody
>;
export declare function resendInviteRequestBodyToJSON(
  resendInviteRequestBody: ResendInviteRequestBody,
): string;
//# sourceMappingURL=resendinviterequestbody.d.ts.map
