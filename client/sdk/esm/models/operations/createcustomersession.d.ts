import * as z from "zod/v4-mini";
export type CreateCustomerSessionSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type CreateCustomerSessionRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type CreateCustomerSessionSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateCustomerSessionSecurity$outboundSchema: z.ZodMiniType<CreateCustomerSessionSecurity$Outbound, CreateCustomerSessionSecurity>;
export declare function createCustomerSessionSecurityToJSON(createCustomerSessionSecurity: CreateCustomerSessionSecurity): string;
/** @internal */
export type CreateCustomerSessionRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const CreateCustomerSessionRequest$outboundSchema: z.ZodMiniType<CreateCustomerSessionRequest$Outbound, CreateCustomerSessionRequest>;
export declare function createCustomerSessionRequestToJSON(createCustomerSessionRequest: CreateCustomerSessionRequest): string;
//# sourceMappingURL=createcustomersession.d.ts.map