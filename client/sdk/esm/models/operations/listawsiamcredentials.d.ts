import * as z from "zod/v4-mini";
export type ListAwsIamCredentialsSecurity = {
    sessionHeaderGramSession?: string | undefined;
};
export type ListAwsIamCredentialsRequest = {
    /**
     * Session header
     */
    gramSession?: string | undefined;
};
/** @internal */
export type ListAwsIamCredentialsSecurity$Outbound = {
    "session_header_Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAwsIamCredentialsSecurity$outboundSchema: z.ZodMiniType<ListAwsIamCredentialsSecurity$Outbound, ListAwsIamCredentialsSecurity>;
export declare function listAwsIamCredentialsSecurityToJSON(listAwsIamCredentialsSecurity: ListAwsIamCredentialsSecurity): string;
/** @internal */
export type ListAwsIamCredentialsRequest$Outbound = {
    "Gram-Session"?: string | undefined;
};
/** @internal */
export declare const ListAwsIamCredentialsRequest$outboundSchema: z.ZodMiniType<ListAwsIamCredentialsRequest$Outbound, ListAwsIamCredentialsRequest>;
export declare function listAwsIamCredentialsRequestToJSON(listAwsIamCredentialsRequest: ListAwsIamCredentialsRequest): string;
//# sourceMappingURL=listawsiamcredentials.d.ts.map