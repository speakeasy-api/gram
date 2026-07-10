import * as z from "zod/v4-mini";
export type CheckMcpEndpointSlugAvailabilitySecurityOption1 = {
    projectSlugHeaderGramProject: string;
    sessionHeaderGramSession: string;
};
export type CheckMcpEndpointSlugAvailabilitySecurityOption2 = {
    apikeyHeaderGramKey: string;
    projectSlugHeaderGramProject: string;
};
export type CheckMcpEndpointSlugAvailabilitySecurity = {
    option1?: CheckMcpEndpointSlugAvailabilitySecurityOption1 | undefined;
    option2?: CheckMcpEndpointSlugAvailabilitySecurityOption2 | undefined;
};
export type CheckMcpEndpointSlugAvailabilityRequest = {
    /**
     * The slug to check
     */
    slug: string;
    /**
     * Optional custom domain ID. Omit to check platform-domain slug availability.
     */
    customDomainId?: string | undefined;
    /**
     * Session header
     */
    gramSession?: string | undefined;
    /**
     * API Key header
     */
    gramKey?: string | undefined;
    /**
     * project header
     */
    gramProject?: string | undefined;
};
/** @internal */
export type CheckMcpEndpointSlugAvailabilitySecurityOption1$Outbound = {
    "project_slug_header_Gram-Project": string;
    "session_header_Gram-Session": string;
};
/** @internal */
export declare const CheckMcpEndpointSlugAvailabilitySecurityOption1$outboundSchema: z.ZodMiniType<CheckMcpEndpointSlugAvailabilitySecurityOption1$Outbound, CheckMcpEndpointSlugAvailabilitySecurityOption1>;
export declare function checkMcpEndpointSlugAvailabilitySecurityOption1ToJSON(checkMcpEndpointSlugAvailabilitySecurityOption1: CheckMcpEndpointSlugAvailabilitySecurityOption1): string;
/** @internal */
export type CheckMcpEndpointSlugAvailabilitySecurityOption2$Outbound = {
    "apikey_header_Gram-Key": string;
    "project_slug_header_Gram-Project": string;
};
/** @internal */
export declare const CheckMcpEndpointSlugAvailabilitySecurityOption2$outboundSchema: z.ZodMiniType<CheckMcpEndpointSlugAvailabilitySecurityOption2$Outbound, CheckMcpEndpointSlugAvailabilitySecurityOption2>;
export declare function checkMcpEndpointSlugAvailabilitySecurityOption2ToJSON(checkMcpEndpointSlugAvailabilitySecurityOption2: CheckMcpEndpointSlugAvailabilitySecurityOption2): string;
/** @internal */
export type CheckMcpEndpointSlugAvailabilitySecurity$Outbound = {
    Option1?: CheckMcpEndpointSlugAvailabilitySecurityOption1$Outbound | undefined;
    Option2?: CheckMcpEndpointSlugAvailabilitySecurityOption2$Outbound | undefined;
};
/** @internal */
export declare const CheckMcpEndpointSlugAvailabilitySecurity$outboundSchema: z.ZodMiniType<CheckMcpEndpointSlugAvailabilitySecurity$Outbound, CheckMcpEndpointSlugAvailabilitySecurity>;
export declare function checkMcpEndpointSlugAvailabilitySecurityToJSON(checkMcpEndpointSlugAvailabilitySecurity: CheckMcpEndpointSlugAvailabilitySecurity): string;
/** @internal */
export type CheckMcpEndpointSlugAvailabilityRequest$Outbound = {
    slug: string;
    custom_domain_id?: string | undefined;
    "Gram-Session"?: string | undefined;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const CheckMcpEndpointSlugAvailabilityRequest$outboundSchema: z.ZodMiniType<CheckMcpEndpointSlugAvailabilityRequest$Outbound, CheckMcpEndpointSlugAvailabilityRequest>;
export declare function checkMcpEndpointSlugAvailabilityRequestToJSON(checkMcpEndpointSlugAvailabilityRequest: CheckMcpEndpointSlugAvailabilityRequest): string;
//# sourceMappingURL=checkmcpendpointslugavailability.d.ts.map