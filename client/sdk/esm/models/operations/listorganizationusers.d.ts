import * as z from "zod/v4-mini";
export type ListOrganizationUsersSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type ListOrganizationUsersRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type ListOrganizationUsersSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationUsersSecurity$outboundSchema: z.ZodMiniType<ListOrganizationUsersSecurity$Outbound, ListOrganizationUsersSecurity>;
export declare function listOrganizationUsersSecurityToJSON(listOrganizationUsersSecurity: ListOrganizationUsersSecurity): string;
/** @internal */
export type ListOrganizationUsersRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListOrganizationUsersRequest$outboundSchema: z.ZodMiniType<ListOrganizationUsersRequest$Outbound, ListOrganizationUsersRequest>;
export declare function listOrganizationUsersRequestToJSON(listOrganizationUsersRequest: ListOrganizationUsersRequest): string;
//# sourceMappingURL=listorganizationusers.d.ts.map