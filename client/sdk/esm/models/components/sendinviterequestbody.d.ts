import * as z from "zod/v4-mini";
export type SendInviteRequestBody = {
    /**
     * Email address to invite.
     */
    email: string;
    /**
     * Optional role ID for the invitee.
     */
    roleId?: string | undefined;
};
/** @internal */
export type SendInviteRequestBody$Outbound = {
    email: string;
    role_id?: string | undefined;
};
/** @internal */
export declare const SendInviteRequestBody$outboundSchema: z.ZodMiniType<SendInviteRequestBody$Outbound, SendInviteRequestBody>;
export declare function sendInviteRequestBodyToJSON(sendInviteRequestBody: SendInviteRequestBody): string;
//# sourceMappingURL=sendinviterequestbody.d.ts.map