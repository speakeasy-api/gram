import * as z from "zod/v4-mini";
export type RevokeInviteSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type RevokeInviteRequest = {
    /**
     * WorkOS invitation ID.
     */
    invitationId: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type RevokeInviteSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RevokeInviteSecurity$outboundSchema: z.ZodMiniType<RevokeInviteSecurity$Outbound, RevokeInviteSecurity>;
export declare function revokeInviteSecurityToJSON(revokeInviteSecurity: RevokeInviteSecurity): string;
/** @internal */
export type RevokeInviteRequest$Outbound = {
    invitation_id: string;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RevokeInviteRequest$outboundSchema: z.ZodMiniType<RevokeInviteRequest$Outbound, RevokeInviteRequest>;
export declare function revokeInviteRequestToJSON(revokeInviteRequest: RevokeInviteRequest): string;
//# sourceMappingURL=revokeinvite.d.ts.map