import * as z from "zod/v4-mini";
export type CreateCheckoutSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type CreateCheckoutRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type CreateCheckoutSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateCheckoutSecurity$outboundSchema: z.ZodMiniType<CreateCheckoutSecurity$Outbound, CreateCheckoutSecurity>;
export declare function createCheckoutSecurityToJSON(createCheckoutSecurity: CreateCheckoutSecurity): string;
/** @internal */
export type CreateCheckoutRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateCheckoutRequest$outboundSchema: z.ZodMiniType<CreateCheckoutRequest$Outbound, CreateCheckoutRequest>;
export declare function createCheckoutRequestToJSON(createCheckoutRequest: CreateCheckoutRequest): string;
//# sourceMappingURL=createcheckout.d.ts.map