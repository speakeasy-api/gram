import * as z from "zod/v4-mini";
export type EnableRBACSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type EnableRBACRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type EnableRBACSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const EnableRBACSecurity$outboundSchema: z.ZodMiniType<EnableRBACSecurity$Outbound, EnableRBACSecurity>;
export declare function enableRBACSecurityToJSON(enableRBACSecurity: EnableRBACSecurity): string;
/** @internal */
export type EnableRBACRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const EnableRBACRequest$outboundSchema: z.ZodMiniType<EnableRBACRequest$Outbound, EnableRBACRequest>;
export declare function enableRBACRequestToJSON(enableRBACRequest: EnableRBACRequest): string;
//# sourceMappingURL=enablerbac.d.ts.map