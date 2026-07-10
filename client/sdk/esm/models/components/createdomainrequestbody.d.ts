import * as z from "zod/v4-mini";
export type CreateDomainRequestBody = {
    /**
     * The custom domain
     */
    domain: string;
    /**
     * IP addresses or CIDR ranges to allow. Leave empty for unrestricted access.
     */
    ipAllowlist?: Array<string> | undefined;
};
/** @internal */
export type CreateDomainRequestBody$Outbound = {
    domain: string;
    ip_allowlist?: Array<string> | undefined;
};
/** @internal */
export declare const CreateDomainRequestBody$outboundSchema: z.ZodMiniType<CreateDomainRequestBody$Outbound, CreateDomainRequestBody>;
export declare function createDomainRequestBodyToJSON(createDomainRequestBody: CreateDomainRequestBody): string;
//# sourceMappingURL=createdomainrequestbody.d.ts.map