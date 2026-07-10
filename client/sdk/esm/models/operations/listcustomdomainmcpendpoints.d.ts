import * as z from "zod/v4-mini";
export type ListCustomDomainMcpEndpointsSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type ListCustomDomainMcpEndpointsRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type ListCustomDomainMcpEndpointsSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListCustomDomainMcpEndpointsSecurity$outboundSchema: z.ZodMiniType<ListCustomDomainMcpEndpointsSecurity$Outbound, ListCustomDomainMcpEndpointsSecurity>;
export declare function listCustomDomainMcpEndpointsSecurityToJSON(listCustomDomainMcpEndpointsSecurity: ListCustomDomainMcpEndpointsSecurity): string;
/** @internal */
export type ListCustomDomainMcpEndpointsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListCustomDomainMcpEndpointsRequest$outboundSchema: z.ZodMiniType<ListCustomDomainMcpEndpointsRequest$Outbound, ListCustomDomainMcpEndpointsRequest>;
export declare function listCustomDomainMcpEndpointsRequestToJSON(listCustomDomainMcpEndpointsRequest: ListCustomDomainMcpEndpointsRequest): string;
//# sourceMappingURL=listcustomdomainmcpendpoints.d.ts.map