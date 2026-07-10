import * as z from "zod/v4-mini";
export type GetRBACStatusSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type GetRBACStatusRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type GetRBACStatusSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetRBACStatusSecurity$outboundSchema: z.ZodMiniType<GetRBACStatusSecurity$Outbound, GetRBACStatusSecurity>;
export declare function getRBACStatusSecurityToJSON(getRBACStatusSecurity: GetRBACStatusSecurity): string;
/** @internal */
export type GetRBACStatusRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const GetRBACStatusRequest$outboundSchema: z.ZodMiniType<GetRBACStatusRequest$Outbound, GetRBACStatusRequest>;
export declare function getRBACStatusRequestToJSON(getRBACStatusRequest: GetRBACStatusRequest): string;
//# sourceMappingURL=getrbacstatus.d.ts.map