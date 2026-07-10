import * as z from "zod/v4-mini";
export type GetTokensUnderManagementSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type GetTokensUnderManagementRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type GetTokensUnderManagementSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetTokensUnderManagementSecurity$outboundSchema: z.ZodMiniType<GetTokensUnderManagementSecurity$Outbound, GetTokensUnderManagementSecurity>;
export declare function getTokensUnderManagementSecurityToJSON(getTokensUnderManagementSecurity: GetTokensUnderManagementSecurity): string;
/** @internal */
export type GetTokensUnderManagementRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetTokensUnderManagementRequest$outboundSchema: z.ZodMiniType<GetTokensUnderManagementRequest$Outbound, GetTokensUnderManagementRequest>;
export declare function getTokensUnderManagementRequestToJSON(getTokensUnderManagementRequest: GetTokensUnderManagementRequest): string;
//# sourceMappingURL=gettokensundermanagement.d.ts.map