import * as z from "zod/v4-mini";
export type RevokeAPIKeySecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type RevokeAPIKeyRequest = {
    /**
     * The ID of the key to revoke
     */
    id: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type RevokeAPIKeySecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RevokeAPIKeySecurity$outboundSchema: z.ZodMiniType<RevokeAPIKeySecurity$Outbound, RevokeAPIKeySecurity>;
export declare function revokeAPIKeySecurityToJSON(revokeAPIKeySecurity: RevokeAPIKeySecurity): string;
/** @internal */
export type RevokeAPIKeyRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const RevokeAPIKeyRequest$outboundSchema: z.ZodMiniType<RevokeAPIKeyRequest$Outbound, RevokeAPIKeyRequest>;
export declare function revokeAPIKeyRequestToJSON(revokeAPIKeyRequest: RevokeAPIKeyRequest): string;
//# sourceMappingURL=revokeapikey.d.ts.map