import * as z from "zod/v4-mini";
export type RegisterRequestBody = {
    /**
     * The name of the org to register
     */
    orgName: string;
};
/** @internal */
export type RegisterRequestBody$Outbound = {
    org_name: string;
};
/** @internal */
export declare const RegisterRequestBody$outboundSchema: z.ZodMiniType<RegisterRequestBody$Outbound, RegisterRequestBody>;
export declare function registerRequestBodyToJSON(registerRequestBody: RegisterRequestBody): string;
//# sourceMappingURL=registerrequestbody.d.ts.map