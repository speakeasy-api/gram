import * as z from "zod/v4-mini";
export type ListRolesSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    sessionHeaderGramSession?: string | undefined;
};
export type ListRolesRequest = {
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
export type ListRolesSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListRolesSecurity$outboundSchema: z.ZodMiniType<ListRolesSecurity$Outbound, ListRolesSecurity>;
export declare function listRolesSecurityToJSON(listRolesSecurity: ListRolesSecurity): string;
/** @internal */
export type ListRolesRequest$Outbound = {
    "Gram-Key"?: string | undefined;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListRolesRequest$outboundSchema: z.ZodMiniType<ListRolesRequest$Outbound, ListRolesRequest>;
export declare function listRolesRequestToJSON(listRolesRequest: ListRolesRequest): string;
//# sourceMappingURL=listroles.d.ts.map