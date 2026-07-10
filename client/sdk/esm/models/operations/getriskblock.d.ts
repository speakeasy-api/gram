import * as z from "zod/v4-mini";
export type GetRiskBlockSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type GetRiskBlockRequest = {
    /**
     * The block ID (the underlying risk result ID).
     */
    id: string;
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type GetRiskBlockSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetRiskBlockSecurity$outboundSchema: z.ZodMiniType<GetRiskBlockSecurity$Outbound, GetRiskBlockSecurity>;
export declare function getRiskBlockSecurityToJSON(getRiskBlockSecurity: GetRiskBlockSecurity): string;
/** @internal */
export type GetRiskBlockRequest$Outbound = {
    id: string;
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetRiskBlockRequest$outboundSchema: z.ZodMiniType<GetRiskBlockRequest$Outbound, GetRiskBlockRequest>;
export declare function getRiskBlockRequestToJSON(getRiskBlockRequest: GetRiskBlockRequest): string;
//# sourceMappingURL=getriskblock.d.ts.map