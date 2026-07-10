import * as z from "zod/v4-mini";
export type DeleteResponseSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    projectSlugHeaderGramProject?: string | undefined;
};
export type DeleteResponseRequest = {
    /**
     * The ID of the response to retrieve
     */
    responseId: string;
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
export type DeleteResponseSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteResponseSecurity$outboundSchema: z.ZodMiniType<DeleteResponseSecurity$Outbound, DeleteResponseSecurity>;
export declare function deleteResponseSecurityToJSON(deleteResponseSecurity: DeleteResponseSecurity): string;
/** @internal */
export type DeleteResponseRequest$Outbound = {
    response_id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const DeleteResponseRequest$outboundSchema: z.ZodMiniType<DeleteResponseRequest$Outbound, DeleteResponseRequest>;
export declare function deleteResponseRequestToJSON(deleteResponseRequest: DeleteResponseRequest): string;
//# sourceMappingURL=deleteresponse.d.ts.map