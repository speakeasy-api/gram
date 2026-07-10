import * as z from "zod/v4-mini";
export type SetOrganizationWhitelistRequestBody = {
    /**
     * The ID of the organization to update
     */
    organizationId: string;
    /**
     * Whether the organization should be whitelisted
     */
    whitelisted: boolean;
};
/** @internal */
export type SetOrganizationWhitelistRequestBody$Outbound = {
    organization_id: string;
    whitelisted: boolean;
};
/** @internal */
export declare const SetOrganizationWhitelistRequestBody$outboundSchema: z.ZodMiniType<SetOrganizationWhitelistRequestBody$Outbound, SetOrganizationWhitelistRequestBody>;
export declare function setOrganizationWhitelistRequestBodyToJSON(setOrganizationWhitelistRequestBody: SetOrganizationWhitelistRequestBody): string;
//# sourceMappingURL=setorganizationwhitelistrequestbody.d.ts.map