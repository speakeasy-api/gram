import * as z from "zod/v4-mini";
export type GetResponseSecurity = {
    apikeyHeaderGramKey?: string | undefined;
    projectSlugHeaderGramProject?: string | undefined;
};
export type GetResponseRequest = {
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
export type GetResponseSecurity$Outbound = {
    "apikey_header_Gram-Key"?: string | undefined;
    "project_slug_header_Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetResponseSecurity$outboundSchema: z.ZodMiniType<GetResponseSecurity$Outbound, GetResponseSecurity>;
export declare function getResponseSecurityToJSON(getResponseSecurity: GetResponseSecurity): string;
/** @internal */
export type GetResponseRequest$Outbound = {
    response_id: string;
    "Gram-Key"?: string | undefined;
    "Gram-Project"?: string | undefined;
};
/** @internal */
export declare const GetResponseRequest$outboundSchema: z.ZodMiniType<GetResponseRequest$Outbound, GetResponseRequest>;
export declare function getResponseRequestToJSON(getResponseRequest: GetResponseRequest): string;
//# sourceMappingURL=getresponse.d.ts.map