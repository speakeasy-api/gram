import * as z from "zod/v4-mini";
export type ListInvitesSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type ListInvitesRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type ListInvitesSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListInvitesSecurity$outboundSchema: z.ZodMiniType<ListInvitesSecurity$Outbound, ListInvitesSecurity>;
export declare function listInvitesSecurityToJSON(listInvitesSecurity: ListInvitesSecurity): string;
/** @internal */
export type ListInvitesRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListInvitesRequest$outboundSchema: z.ZodMiniType<ListInvitesRequest$Outbound, ListInvitesRequest>;
export declare function listInvitesRequestToJSON(listInvitesRequest: ListInvitesRequest): string;
//# sourceMappingURL=listinvites.d.ts.map