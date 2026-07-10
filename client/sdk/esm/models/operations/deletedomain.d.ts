import * as z from "zod/v4-mini";
export type DeleteDomainSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type DeleteDomainRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type DeleteDomainSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteDomainSecurity$outboundSchema: z.ZodMiniType<DeleteDomainSecurity$Outbound, DeleteDomainSecurity>;
export declare function deleteDomainSecurityToJSON(deleteDomainSecurity: DeleteDomainSecurity): string;
/** @internal */
export type DeleteDomainRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const DeleteDomainRequest$outboundSchema: z.ZodMiniType<DeleteDomainRequest$Outbound, DeleteDomainRequest>;
export declare function deleteDomainRequestToJSON(deleteDomainRequest: DeleteDomainRequest): string;
//# sourceMappingURL=deletedomain.d.ts.map