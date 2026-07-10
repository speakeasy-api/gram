import * as z from "zod/v4-mini";
export type UpdateDomainRequestBody = {
    /**
     * Replacement IP allowlist. Pass an empty list to remove all restrictions.
     */
    ipAllowlist: Array<string>;
};
/** @internal */
export type UpdateDomainRequestBody$Outbound = {
    ip_allowlist: Array<string>;
};
/** @internal */
export declare const UpdateDomainRequestBody$outboundSchema: z.ZodMiniType<UpdateDomainRequestBody$Outbound, UpdateDomainRequestBody>;
export declare function updateDomainRequestBodyToJSON(updateDomainRequestBody: UpdateDomainRequestBody): string;
//# sourceMappingURL=updatedomainrequestbody.d.ts.map