import * as z from "zod/v4-mini";
export type ListAllOrganizationsSecurity = {
    apikeyHeaderGramKey?: string | undefined;
};
export type ListAllOrganizationsRequest = {
    /**
     * Maximum organizations to return (default 100, max 500).
     */
    limit?: number | undefined;
    /**
     * Number of organizations to skip.
     */
    offset?: number | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
};
/** @internal */
export type ListAllOrganizationsSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListAllOrganizationsSecurity$outboundSchema: z.ZodMiniType<ListAllOrganizationsSecurity$Outbound, ListAllOrganizationsSecurity>;
export declare function listAllOrganizationsSecurityToJSON(listAllOrganizationsSecurity: ListAllOrganizationsSecurity): string;
/** @internal */
export type ListAllOrganizationsRequest$Outbound = {
    limit?: number | undefined;
    offset?: number | undefined;
    "Gram-Key"?: string | undefined;
};
/** @internal */
export declare const ListAllOrganizationsRequest$outboundSchema: z.ZodMiniType<ListAllOrganizationsRequest$Outbound, ListAllOrganizationsRequest>;
export declare function listAllOrganizationsRequestToJSON(listAllOrganizationsRequest: ListAllOrganizationsRequest): string;
//# sourceMappingURL=listallorganizations.d.ts.map