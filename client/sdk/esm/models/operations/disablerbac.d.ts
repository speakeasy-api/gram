import * as z from "zod/v4-mini";
export type DisableRBACSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type DisableRBACRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type DisableRBACSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DisableRBACSecurity$outboundSchema: z.ZodMiniType<DisableRBACSecurity$Outbound, DisableRBACSecurity>;
export declare function disableRBACSecurityToJSON(disableRBACSecurity: DisableRBACSecurity): string;
/** @internal */
export type DisableRBACRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DisableRBACRequest$outboundSchema: z.ZodMiniType<DisableRBACRequest$Outbound, DisableRBACRequest>;
export declare function disableRBACRequestToJSON(disableRBACRequest: DisableRBACRequest): string;
//# sourceMappingURL=disablerbac.d.ts.map